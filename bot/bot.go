package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- Telegram wire types (minimal) -----------------------------------------

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	MessageID int     `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUpdate struct {
	UpdateID int        `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

// --- Bot --------------------------------------------------------------------

const helpText = `openclaw
Send any message and I'll pass it to Claude Code running on the server.

Commands:
/new — start a fresh Claude session
/status — show current session info
/help — show this message`

const maxTelegramMessage = 3800

type Bot struct {
	token  string
	client *http.Client
	state  *State
	model  string
	offset int
}

func NewBot(token string, state *State, model string) *Bot {
	return &Bot{
		token: token,
		// 65s: longer than our long-poll timeout so requests don't race the
		// HTTP client's own deadline.
		client: &http.Client{Timeout: 65 * time.Second},
		state:  state,
		model:  model,
	}
}

func (b *Bot) api(ctx context.Context, method string, params url.Values) (json.RawMessage, error) {
	endpoint := "https://api.telegram.org/bot" + b.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r tgResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("tg decode %s: %w", method, err)
	}
	if !r.OK {
		return nil, fmt.Errorf("tg %s: %s", method, r.Description)
	}
	return r.Result, nil
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	params.Set("disable_web_page_preview", "true")
	_, err := b.api(ctx, "sendMessage", params)
	return err
}

func (b *Bot) typing(ctx context.Context, chatID int64) {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("action", "typing")
	_, _ = b.api(ctx, "sendChatAction", params)
}

func (b *Bot) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	params := url.Values{}
	params.Set("timeout", "50")
	params.Set("offset", strconv.Itoa(b.offset))
	params.Set("allowed_updates", `["message"]`)
	raw, err := b.api(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	var updates []tgUpdate
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// Run long-polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	log.Printf("bot: long-polling started (allowed=%v model=%q)", b.state.Allowed, b.model)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("bot: getUpdates: %v (retrying in %s)", err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			b.offset = u.UpdateID + 1
			if u.Message != nil {
				go b.handleMessage(ctx, u.Message)
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *tgMessage) {
	if m.From == nil {
		return
	}
	uid := m.From.ID

	if !b.state.IsAllowed(uid) {
		log.Printf("deny unauthorized user %d", uid)
		_ = b.send(ctx, m.Chat.ID, fmt.Sprintf(
			"Unauthorized. Your Telegram user id is %d. Ask the operator to add it to the allowlist.",
			uid,
		))
		return
	}

	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}

	switch text {
	case "/start", "/help":
		_ = b.send(ctx, m.Chat.ID, helpText)
		return
	case "/new":
		sess := b.state.Session(uid)
		sess.mu.Lock()
		sess.SessionID = ""
		sess.mu.Unlock()
		_ = b.send(ctx, m.Chat.ID, "🧹 New Claude session started.")
		return
	case "/status":
		sess := b.state.Session(uid)
		sid := sess.SessionID
		if sid == "" {
			sid = "(none)"
		}
		model := b.model
		if model == "" {
			model = "(default)"
		}
		_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("session_id: %s\ncwd: %s\nmodel: %s", sid, sess.Cwd, model))
		return
	}

	sess := b.state.Session(uid)

	// Only one claude call per user at a time. If they spam us, bounce the
	// second message back cheaply.
	if !sess.mu.TryLock() {
		_ = b.send(ctx, m.Chat.ID, "⏳ Previous request still running, hang on…")
		return
	}
	defer sess.mu.Unlock()

	b.state.Record(uid, "in", text)
	b.typing(ctx, m.Chat.ID)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	reply, err := runClaude(callCtx, sess, b.model, text)
	if err != nil {
		log.Printf("claude error: %v", err)
		reply = "❌ " + err.Error()
		b.state.Record(uid, "error", reply)
	} else {
		b.state.Record(uid, "out", reply)
	}

	for _, chunk := range chunks(reply, maxTelegramMessage) {
		if err := b.send(ctx, m.Chat.ID, chunk); err != nil {
			log.Printf("send: %v", err)
			return
		}
	}
}

func chunks(s string, n int) []string {
	if s == "" {
		return []string{"(empty)"}
	}
	r := []rune(s)
	var out []string
	for i := 0; i < len(r); i += n {
		end := i + n
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}
