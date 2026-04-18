package main

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"sync"
	"time"
)

// chatMessage is a single message in the web chat history.
type chatMessage struct {
	Role string `json:"role"` // "user" or "assistant"
	Text string `json:"text"`
	Time string `json:"time"`
}

// chatStore keeps per-dashboard-user chat history in memory.
var chatHistory = struct {
	sync.RWMutex
	msgs map[string][]chatMessage // keyed by dashboard email
}{msgs: make(map[string][]chatMessage)}

func getChatHistory(email string) []chatMessage {
	chatHistory.RLock()
	defer chatHistory.RUnlock()
	msgs := chatHistory.msgs[email]
	if msgs == nil {
		return []chatMessage{}
	}
	return msgs
}

func appendChatMessage(email string, msg chatMessage) {
	chatHistory.Lock()
	defer chatHistory.Unlock()
	chatHistory.msgs[email] = append(chatHistory.msgs[email], msg)
	// Cap at 100 messages per user.
	if len(chatHistory.msgs[email]) > 100 {
		chatHistory.msgs[email] = chatHistory.msgs[email][len(chatHistory.msgs[email])-100:]
	}
}

// handleChat serves the web chat UI.
func (d *dashboardServer) handleChat(w http.ResponseWriter, r *http.Request) {
	email := d.sessions.authedEmail(r)
	if email == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	chatPageTmpl.Execute(w, map[string]interface{}{
		"Email": email,
		"CSS":   template.CSS(dashboardCSS),
		"Mark":  template.HTML(brandMarkHTML),
	})
}

// handleChatAPI handles GET (history) and POST (send message) for the chat.
func (d *dashboardServer) handleChatAPI(w http.ResponseWriter, r *http.Request) {
	email := d.sessions.authedEmail(r)
	if email == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "GET":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getChatHistory(email))

	case "POST":
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if d.bot == nil {
			http.Error(w, "bot not available", http.StatusServiceUnavailable)
			return
		}

		// Use a synthetic user ID for web chat users (hash of email).
		uid := webChatUID(email)
		sess := d.bot.state.Session(uid)

		// Record user message.
		userMsg := chatMessage{
			Role: "user",
			Text: req.Message,
			Time: time.Now().Format("15:04:05"),
		}
		appendChatMessage(email, userMsg)
		d.bot.state.Record(uid, "in", req.Message)

		// Run Claude.
		callCtx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		reply, err := runClaude(callCtx, sess, d.bot.model, req.Message)
		if err != nil {
			reply = "Error: " + err.Error()
			d.bot.state.Record(uid, "error", reply)
		} else {
			d.bot.state.Record(uid, "out", reply)
		}

		assistantMsg := chatMessage{
			Role: "assistant",
			Text: reply,
			Time: time.Now().Format("15:04:05"),
		}
		appendChatMessage(email, assistantMsg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(assistantMsg)

	case "DELETE":
		// Clear chat history.
		chatHistory.Lock()
		delete(chatHistory.msgs, email)
		chatHistory.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// webChatUID generates a stable int64 user ID from a dashboard email.
// Uses a simple hash to avoid collision with Telegram user IDs (which are
// typically < 10 billion). We offset into a high range.
func webChatUID(email string) int64 {
	var h int64 = 0x7000000000000000
	for _, c := range email {
		h = h*31 + int64(c)
	}
	if h < 0 {
		h = -h
	}
	return h | 0x7000000000000000
}

var chatPageTmpl = template.Must(template.New("chat").Parse(chatHTML))

const chatHTML = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">
<title>Claw Chat</title>
<style>{{.CSS}}

/* ---- chat-specific ---- */
.chat-wrap {
  max-width: 800px; margin: 0 auto; padding: 0 16px;
  display: flex; flex-direction: column; height: calc(100vh - 60px);
}
.messages {
  flex: 1; overflow-y: auto; padding: 16px 0;
  display: flex; flex-direction: column; gap: 12px;
}
.msg {
  max-width: 85%; padding: 10px 14px; border-radius: 12px;
  font-size: 14px; line-height: 1.6; word-wrap: break-word;
  white-space: pre-wrap;
}
.msg.user {
  align-self: flex-end;
  background: var(--accent); color: #fff;
  border-bottom-right-radius: 4px;
}
.msg.assistant {
  align-self: flex-start;
  background: var(--card-2); color: var(--fg);
  border-bottom-left-radius: 4px;
  border: 1px solid var(--border);
}
.msg .time { font-size: 11px; color: var(--muted); margin-top: 4px; }
.msg.user .time { color: rgba(255,255,255,0.6); }

.input-bar {
  display: flex; gap: 8px; padding: 12px 0 16px;
  border-top: 1px solid var(--border);
}
.input-bar textarea {
  flex: 1; background: var(--card); color: var(--fg);
  border: 1px solid var(--border); border-radius: 10px;
  padding: 10px 14px; font-size: 14px; font-family: inherit;
  resize: none; min-height: 44px; max-height: 120px;
  outline: none;
}
.input-bar textarea:focus { border-color: var(--accent); }
.input-bar button {
  background: var(--accent); color: #fff; border: none;
  border-radius: 10px; padding: 10px 20px; font-size: 14px;
  font-weight: 600; cursor: pointer; white-space: nowrap;
}
.input-bar button:hover { background: var(--accent-2); }
.input-bar button:disabled { opacity: 0.5; cursor: not-allowed; }

.typing { color: var(--muted); font-size: 13px; padding: 4px 0; }
.empty-state {
  flex: 1; display: flex; align-items: center; justify-content: center;
  flex-direction: column; gap: 12px; color: var(--muted);
}
.empty-state .brand-mark { width: 64px; height: 64px; opacity: 0.3; }
</style>
</head>
<body>
<nav class="nav">
  <div class="nav-inner">
    <div class="brand">
      <span class="brand-mark">{{.Mark}}</span>
      <span>claw</span>
    </div>
    <a class="tab" href="/">Dashboard</a>
    <a class="tab" href="/chat" style="background:var(--card);">Chat</a>
    <span class="spacer"></span>
    <span class="who">
      {{.Email}}
      <form method="POST" action="/logout" style="margin:0"><button type="submit" style="background:none;border:none;color:var(--muted);cursor:pointer;font-size:12px;">logout</button></form>
    </span>
  </div>
</nav>

<div class="chat-wrap">
  <div class="messages" id="messages">
    <div class="empty-state" id="empty">
      <span class="brand-mark">{{.Mark}}</span>
      <div>Send a message to start chatting with Claude</div>
    </div>
  </div>
  <div class="typing" id="typing" style="display:none">Claude is thinking...</div>
  <div class="input-bar">
    <textarea id="input" placeholder="Message Claude..." rows="1"></textarea>
    <button id="send" onclick="sendMessage()">Send</button>
  </div>
</div>

<script>
const messagesEl = document.getElementById('messages');
const inputEl = document.getElementById('input');
const sendBtn = document.getElementById('send');
const typingEl = document.getElementById('typing');
const emptyEl = document.getElementById('empty');

// Auto-resize textarea.
inputEl.addEventListener('input', function() {
  this.style.height = 'auto';
  this.style.height = Math.min(this.scrollHeight, 120) + 'px';
});

// Enter to send (shift+enter for newline).
inputEl.addEventListener('keydown', function(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    sendMessage();
  }
});

function addMessage(role, text, time) {
  if (emptyEl) emptyEl.style.display = 'none';
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  div.textContent = text;
  if (time) {
    const t = document.createElement('div');
    t.className = 'time';
    t.textContent = time;
    div.appendChild(t);
  }
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

async function sendMessage() {
  const text = inputEl.value.trim();
  if (!text) return;

  inputEl.value = '';
  inputEl.style.height = 'auto';
  sendBtn.disabled = true;
  typingEl.style.display = 'block';

  addMessage('user', text, new Date().toTimeString().slice(0,8));

  try {
    const resp = await fetch('/api/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({message: text}),
    });
    if (!resp.ok) throw new Error(await resp.text());
    const msg = await resp.json();
    addMessage('assistant', msg.text, msg.time);
  } catch (err) {
    addMessage('assistant', 'Error: ' + err.message, '');
  }

  sendBtn.disabled = false;
  typingEl.style.display = 'none';
  inputEl.focus();
}

// Load history on page load.
(async function() {
  try {
    const resp = await fetch('/api/chat');
    if (!resp.ok) return;
    const msgs = await resp.json();
    if (msgs && msgs.length > 0) {
      msgs.forEach(m => addMessage(m.role, m.text, m.time));
    }
  } catch (_) {}
})();
</script>
</body></html>`
