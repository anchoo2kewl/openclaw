package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// --- Telegram wire types (minimal) -----------------------------------------

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	MessageID int          `json:"message_id"`
	From      *tgUser      `json:"from"`
	Chat      tgChat       `json:"chat"`
	Text      string       `json:"text"`
	Document  *tgDocument  `json:"document"`
	Photo     []tgPhoto    `json:"photo"`
	Caption   string       `json:"caption"`
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
/clone <owner/repo> — clone a GitHub repo into workspace
/git status — show workspace git status
/git diff — show uncommitted changes
/git log — show recent commits
/git branch <name> — create and switch to a new branch
/pr [title] — commit all changes, push, and create a GitHub PR
/files — list files in workspace
/download <path> — send a file from workspace
/schedule <cron> <prompt> — schedule a recurring task
/jobs — list scheduled jobs
/cancel <id> — cancel a scheduled job
/project <name> — create/switch to a named project
/projects — list all projects
/project delete <name> — delete a project
/tools — list available tool integrations
/tool enable <name> — enable a tool (github, fetch, filesystem)
/tool disable <name> — disable a tool
/history — show recent conversation history
/search <query> — search through past conversations
/orchestrate <strategy> <task> — run multi-agent workflow
/strategies — list available orchestration strategies
/plugin catalog — browse available plugins
/plugin install <name> — install a plugin
/plugin list — show installed plugins
/plugin remove <name> — uninstall a plugin
/plugin custom <name> <cmd> [args] — install custom MCP server
Send any file/photo to save it to the workspace
/help — show this message

Schedule formats: HH:MM (daily UTC) or */N (every N minutes)
Examples: /schedule 09:00 run tests and report
          /schedule */30 check git status`

const (
	maxTelegramMessage = 3800
	// handlerSlots caps how many inbound messages we process concurrently.
	// Per-user claude calls are already serialized by the session mutex, so
	// this mostly limits goroutine + memory pressure from a flood of
	// unauthorized messages we're silently dropping.
	handlerSlots = 16
)

type Bot struct {
	token     string
	client    *http.Client
	state     *State
	model     string
	offset    int
	sem       chan struct{}
	scheduler *Scheduler
	projects  *ProjectStore
	tools     *ToolManager
	history   *HistoryStore
	plugins   *PluginStore
}

func NewBot(token string, state *State, model string) *Bot {
	return &Bot{
		token: token,
		// 65s: longer than our long-poll timeout so requests don't race the
		// HTTP client's own deadline.
		client: &http.Client{Timeout: 65 * time.Second},
		state:  state,
		model:  model,
		sem:    make(chan struct{}, handlerSlots),
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
	log.Info().
		Ints64("allowed", b.state.Allowed).
		Str("model", b.model).
		Msg("telegram long-poll started")
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
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("getUpdates failed")
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
			if u.Message == nil {
				continue
			}
			// Cheap pre-filter: if the sender isn't on the allowlist, drop
			// immediately without spawning anything and without replying.
			// This is the hot path under a flood — no Claude call, no
			// Telegram API call, no goroutine, no log entry.
			if u.Message.From == nil || !b.state.IsAllowed(u.Message.From.ID) {
				continue
			}
			msg := u.Message
			select {
			case b.sem <- struct{}{}:
				go func() {
					defer func() { <-b.sem }()
					b.handleMessage(ctx, msg)
				}()
			default:
				log.Warn().Int64("uid", msg.From.ID).Msg("handler queue full, dropping")
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *tgMessage) {
	// Defence in depth — Run() already dropped non-allowlisted senders, but
	// re-check here so any future refactor can't accidentally skip it.
	if m.From == nil || !b.state.IsAllowed(m.From.ID) {
		return
	}
	uid := m.From.ID

	// Handle incoming file uploads (documents and photos).
	if m.Document != nil {
		sess := b.state.Session(uid)
		fileName := m.Document.FileName
		if fileName == "" {
			fileName = "document"
		}
		b.state.Record(uid, "in", fmt.Sprintf("[file: %s]", fileName))
		b.typing(ctx, m.Chat.ID)
		result, err := b.downloadFile(ctx, m.Document.FileID, fileName, sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, "📥 "+result)
			if m.Caption != "" {
				// If there's a caption, treat it as a message to Claude about the file.
				text := strings.TrimSpace(m.Caption)
				b.handleTextMessage(ctx, m, uid, text)
			}
		}
		return
	}
	if len(m.Photo) > 0 {
		// Pick the largest photo (last in the array).
		photo := m.Photo[len(m.Photo)-1]
		sess := b.state.Session(uid)
		fileName := fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
		b.state.Record(uid, "in", fmt.Sprintf("[photo: %s]", fileName))
		b.typing(ctx, m.Chat.ID)
		result, err := b.downloadFile(ctx, photo.FileID, fileName, sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, "📥 "+result)
			if m.Caption != "" {
				text := strings.TrimSpace(m.Caption)
				b.handleTextMessage(ctx, m, uid, text)
			}
		}
		return
	}

	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}

	b.handleTextMessage(ctx, m, uid, text)
}

func (b *Bot) handleTextMessage(ctx context.Context, m *tgMessage, uid int64, text string) {
	switch {
	case text == "/start" || text == "/help":
		_ = b.send(ctx, m.Chat.ID, helpText)
		return
	case text == "/new":
		sess := b.state.Session(uid)
		sess.mu.Lock()
		sess.SessionID = ""
		sess.mu.Unlock()
		_ = b.send(ctx, m.Chat.ID, "🧹 New Claude session started.")
		return
	case text == "/status":
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
	case strings.HasPrefix(text, "/clone "):
		repo := strings.TrimSpace(strings.TrimPrefix(text, "/clone "))
		if repo == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /clone owner/repo")
			return
		}
		sess := b.state.Session(uid)
		sess.mu.Lock()
		b.typing(ctx, m.Chat.ID)
		b.state.Record(uid, "in", text)
		result, err := gitClone(ctx, sess.Cwd, repo)
		sess.SessionID = "" // reset Claude session for new repo
		sess.mu.Unlock()
		if err != nil {
			b.state.Record(uid, "error", err.Error())
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			b.state.Record(uid, "out", result)
			_ = b.send(ctx, m.Chat.ID, "✅ "+result+"\nClaude session reset for new workspace.")
		}
		return
	case text == "/git status":
		sess := b.state.Session(uid)
		b.state.Record(uid, "in", text)
		result, err := gitStatus(ctx, sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else if result == "" {
			_ = b.send(ctx, m.Chat.ID, "Clean working tree.")
		} else {
			_ = b.send(ctx, m.Chat.ID, result)
		}
		return
	case text == "/git diff":
		sess := b.state.Session(uid)
		b.state.Record(uid, "in", text)
		result, err := gitDiff(ctx, sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			for _, chunk := range chunks(result, maxTelegramMessage) {
				_ = b.send(ctx, m.Chat.ID, chunk)
			}
		}
		return
	case text == "/git log":
		sess := b.state.Session(uid)
		b.state.Record(uid, "in", text)
		result, err := gitLog(ctx, sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, result)
		}
		return
	case strings.HasPrefix(text, "/git branch "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/git branch "))
		if name == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /git branch <name>")
			return
		}
		sess := b.state.Session(uid)
		b.state.Record(uid, "in", text)
		result, err := gitBranch(ctx, sess.Cwd, name)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, "✅ "+result)
		}
		return
	case text == "/pr" || strings.HasPrefix(text, "/pr "):
		title := strings.TrimSpace(strings.TrimPrefix(text, "/pr"))
		sess := b.state.Session(uid)
		sess.mu.Lock()
		b.typing(ctx, m.Chat.ID)
		b.state.Record(uid, "in", text)
		result, err := gitCreatePR(ctx, sess.Cwd, title)
		sess.mu.Unlock()
		if err != nil {
			b.state.Record(uid, "error", err.Error())
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			b.state.Record(uid, "out", result)
			_ = b.send(ctx, m.Chat.ID, "✅ "+result)
		}
		return
	case text == "/files":
		sess := b.state.Session(uid)
		result, err := listFiles(sess.Cwd)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, result)
		}
		return
	case strings.HasPrefix(text, "/download "):
		path := strings.TrimSpace(strings.TrimPrefix(text, "/download "))
		if path == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /download <path>")
			return
		}
		sess := b.state.Session(uid)
		b.typing(ctx, m.Chat.ID)
		if err := b.sendFile(ctx, m.Chat.ID, sess.Cwd, path); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		}
		return
	case strings.HasPrefix(text, "/schedule "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "/schedule "))
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 2 {
			_ = b.send(ctx, m.Chat.ID, "Usage: /schedule <HH:MM or */N> <prompt>")
			return
		}
		if b.scheduler == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Scheduler not available")
			return
		}
		job, err := b.scheduler.Add(uid, m.Chat.ID, parts[0], parts[1])
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Job #%d scheduled (%s): %s", job.ID, job.Cron, job.Prompt))
		}
		return
	case text == "/jobs":
		if b.scheduler == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Scheduler not available")
			return
		}
		jobs := b.scheduler.List(uid)
		if len(jobs) == 0 {
			_ = b.send(ctx, m.Chat.ID, "No scheduled jobs.")
			return
		}
		var sb strings.Builder
		for _, j := range jobs {
			fmt.Fprintf(&sb, "#%d [%s] %s\n", j.ID, j.Cron, j.Prompt)
		}
		_ = b.send(ctx, m.Chat.ID, sb.String())
		return
	case strings.HasPrefix(text, "/cancel "):
		idStr := strings.TrimSpace(strings.TrimPrefix(text, "/cancel "))
		id, err := strconv.Atoi(idStr)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "Usage: /cancel <job-id>")
			return
		}
		if b.scheduler == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Scheduler not available")
			return
		}
		if b.scheduler.Cancel(uid, id) {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Job #%d cancelled.", id))
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("❌ Job #%d not found.", id))
		}
		return
	case text == "/projects":
		if b.projects == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Projects not available")
			return
		}
		sess := b.state.Session(uid)
		userWs := filepath.Join(b.state.Workspace, strconv.FormatInt(uid, 10))
		projects, err := b.projects.ListProjects(userWs)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
			return
		}
		current := b.projects.CurrentProject(uid)
		if len(projects) == 0 {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("No projects yet. Current workspace: %s\nUse /project <name> to create one.", sess.Cwd))
			return
		}
		var sb strings.Builder
		for _, p := range projects {
			marker := "  "
			if p == current {
				marker = "▶ "
			}
			fmt.Fprintf(&sb, "%s%s\n", marker, p)
		}
		_ = b.send(ctx, m.Chat.ID, sb.String())
		return
	case strings.HasPrefix(text, "/project delete "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/project delete "))
		if name == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /project delete <name>")
			return
		}
		if b.projects == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Projects not available")
			return
		}
		userWs := filepath.Join(b.state.Workspace, strconv.FormatInt(uid, 10))
		if err := b.projects.DeleteProject(uid, userWs, name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Project '%s' deleted.", name))
		}
		return
	case strings.HasPrefix(text, "/project "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/project "))
		if name == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /project <name>")
			return
		}
		if b.projects == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Projects not available")
			return
		}
		sess := b.state.Session(uid)
		userWs := filepath.Join(b.state.Workspace, strconv.FormatInt(uid, 10))
		projectDir, err := b.projects.SwitchProject(uid, userWs, name)
		if err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
			return
		}
		sess.mu.Lock()
		sess.Cwd = projectDir
		sess.SessionID = "" // new project = new Claude session
		sess.mu.Unlock()
		_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Switched to project '%s'\nWorkspace: %s\nClaude session reset.", name, projectDir))
		return
	case text == "/tools":
		if b.tools == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Tool manager not available")
			return
		}
		_ = b.send(ctx, m.Chat.ID, b.tools.FormatToolList())
		return
	case strings.HasPrefix(text, "/tool enable "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/tool enable "))
		if b.tools == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Tool manager not available")
			return
		}
		if err := b.tools.EnableTool(name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Tool '%s' enabled. It will be available in your next Claude session.\nUse /new to start a fresh session with the new tool.", name))
		}
		return
	case strings.HasPrefix(text, "/tool disable "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/tool disable "))
		if b.tools == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Tool manager not available")
			return
		}
		if err := b.tools.DisableTool(name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Tool '%s' disabled.", name))
		}
		return
	case text == "/history":
		if b.history == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ History not available")
			return
		}
		entries := b.history.Recent(uid, 20)
		result := FormatEntries(entries)
		for _, chunk := range chunks(result, maxTelegramMessage) {
			_ = b.send(ctx, m.Chat.ID, chunk)
		}
		return
	case strings.HasPrefix(text, "/search "):
		query := strings.TrimSpace(strings.TrimPrefix(text, "/search "))
		if query == "" {
			_ = b.send(ctx, m.Chat.ID, "Usage: /search <query>")
			return
		}
		if b.history == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ History not available")
			return
		}
		entries := b.history.Search(uid, query, 20)
		if len(entries) == 0 {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("No results for '%s'.", query))
			return
		}
		result := fmt.Sprintf("Found %d results for '%s':\n\n%s", len(entries), query, FormatEntries(entries))
		for _, chunk := range chunks(result, maxTelegramMessage) {
			_ = b.send(ctx, m.Chat.ID, chunk)
		}
		return
	case text == "/strategies":
		_ = b.send(ctx, m.Chat.ID, ListStrategies())
		return
	case strings.HasPrefix(text, "/orchestrate "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "/orchestrate "))
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 2 {
			_ = b.send(ctx, m.Chat.ID, "Usage: /orchestrate <strategy> <task>\n\n"+ListStrategies())
			return
		}
		strategyName := strings.ToLower(parts[0])
		task := parts[1]
		strategy, ok := strategies[strategyName]
		if !ok {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("Unknown strategy '%s'.\n\n%s", strategyName, ListStrategies()))
			return
		}

		sess := b.state.Session(uid)
		_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("🚀 Starting %s orchestration with %d agents...", strategy.Name, len(strategy.Agents)))
		b.typing(ctx, m.Chat.ID)
		b.state.Record(uid, "in", text)

		results := Orchestrate(ctx, strategy, task, sess.Cwd, b.model)
		output := FormatOrchestrationResults(strategy.Name, results)

		b.state.Record(uid, "out", output)
		if b.history != nil {
			b.history.Append(uid, "in", text)
			b.history.Append(uid, "out", output)
		}

		for _, chunk := range chunks(output, maxTelegramMessage) {
			_ = b.send(ctx, m.Chat.ID, chunk)
		}
		return
	case text == "/plugin catalog":
		_ = b.send(ctx, m.Chat.ID, FormatCatalog())
		return
	case text == "/plugin list":
		if b.plugins == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Plugin system not available")
			return
		}
		_ = b.send(ctx, m.Chat.ID, b.plugins.FormatPluginList())
		return
	case strings.HasPrefix(text, "/plugin install "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/plugin install "))
		if b.plugins == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Plugin system not available")
			return
		}
		if err := b.plugins.Install(name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Plugin '%s' installed.\nUse /new to start a session with the new plugin.", name))
		}
		return
	case strings.HasPrefix(text, "/plugin remove "):
		name := strings.TrimSpace(strings.TrimPrefix(text, "/plugin remove "))
		if b.plugins == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Plugin system not available")
			return
		}
		if err := b.plugins.Remove(name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Plugin '%s' removed.", name))
		}
		return
	case strings.HasPrefix(text, "/plugin custom "):
		rest := strings.TrimSpace(strings.TrimPrefix(text, "/plugin custom "))
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			_ = b.send(ctx, m.Chat.ID, "Usage: /plugin custom <name> <command> [args...]")
			return
		}
		if b.plugins == nil {
			_ = b.send(ctx, m.Chat.ID, "❌ Plugin system not available")
			return
		}
		name := parts[0]
		command := parts[1]
		args := parts[2:]
		if err := b.plugins.InstallCustom(name, command, args, "Custom plugin: "+name); err != nil {
			_ = b.send(ctx, m.Chat.ID, "❌ "+err.Error())
		} else {
			_ = b.send(ctx, m.Chat.ID, fmt.Sprintf("✅ Custom plugin '%s' installed (%s %s).\nUse /new to start a session with it.", name, command, strings.Join(args, " ")))
		}
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
	if b.history != nil {
		b.history.Append(uid, "in", text)
	}
	b.typing(ctx, m.Chat.ID)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	reply, err := runClaude(callCtx, sess, b.model, text)
	if err != nil {
		log.Error().Err(err).Int64("uid", uid).Msg("claude failed")
		reply = "❌ " + err.Error()
		b.state.Record(uid, "error", reply)
		if b.history != nil {
			b.history.Append(uid, "error", reply)
		}
	} else {
		b.state.Record(uid, "out", reply)
		if b.history != nil {
			b.history.Append(uid, "out", reply)
		}
	}

	for _, chunk := range chunks(reply, maxTelegramMessage) {
		if err := b.send(ctx, m.Chat.ID, chunk); err != nil {
			log.Warn().Err(err).Int64("uid", uid).Msg("telegram send failed")
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
