package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ---------- async job runner -------------------------------------------------

// RunJob represents an async Claude Code run triggered via the API.
type RunJob struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // "pending", "running", "done", "error"
	Prompt    string `json:"prompt"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
	DoneAt    string `json:"done_at,omitempty"`
}

// JobRunner manages async Claude Code runs.
type JobRunner struct {
	mu   sync.RWMutex
	jobs map[string]*RunJob
	bot  *Bot
}

func NewJobRunner(bot *Bot) *JobRunner {
	return &JobRunner{
		jobs: make(map[string]*RunJob),
		bot:  bot,
	}
}

func (jr *JobRunner) Submit(prompt, workspace string) *RunJob {
	id := fmt.Sprintf("run_%d", time.Now().UnixNano())
	job := &RunJob{
		ID:        id,
		Status:    "pending",
		Prompt:    prompt,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	jr.mu.Lock()
	jr.jobs[id] = job
	jr.mu.Unlock()

	go jr.execute(job, workspace)
	return job
}

func (jr *JobRunner) execute(job *RunJob, workspace string) {
	jr.mu.Lock()
	job.Status = "running"
	jr.mu.Unlock()

	// Use a dedicated session for API runs.
	sess := &Session{
		UserID: 0, // API user
		Cwd:    workspace,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := runClaude(ctx, sess, jr.bot.model, job.Prompt)

	jr.mu.Lock()
	defer jr.mu.Unlock()
	job.DoneAt = time.Now().Format(time.RFC3339)
	if err != nil {
		job.Status = "error"
		job.Error = err.Error()
		log.Error().Err(err).Str("job_id", job.ID).Msg("api run failed")
	} else {
		job.Status = "done"
		job.Result = result
	}
}

func (jr *JobRunner) Get(id string) *RunJob {
	jr.mu.RLock()
	defer jr.mu.RUnlock()
	return jr.jobs[id]
}

func (jr *JobRunner) ListRecent(n int) []*RunJob {
	jr.mu.RLock()
	defer jr.mu.RUnlock()
	out := make([]*RunJob, 0, len(jr.jobs))
	for _, j := range jr.jobs {
		out = append(out, j)
	}
	// Simple: return last n (map order is random, but fine for now).
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// ---------- API handlers -----------------------------------------------------

// apiAuth checks bearer token auth for API endpoints.
func apiAuth(r *http.Request) bool {
	token := os.Getenv("API_TOKEN")
	if token == "" {
		return false // API disabled if no token set
	}
	auth := r.Header.Get("Authorization")
	return auth == "Bearer "+token
}

// handleAPIRun handles POST /api/run (submit) and GET /api/run?id=<id> (poll).
func (d *dashboardServer) handleAPIRun(w http.ResponseWriter, r *http.Request) {
	// Accept both API token and dashboard cookie auth.
	if !apiAuth(r) && d.sessions.authedEmail(r) == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "POST":
		var req struct {
			Prompt    string `json:"prompt"`
			Workspace string `json:"workspace,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
			http.Error(w, `{"error":"prompt is required"}`, http.StatusBadRequest)
			return
		}
		if d.bot == nil || d.jobRunner == nil {
			http.Error(w, `{"error":"bot not available"}`, http.StatusServiceUnavailable)
			return
		}

		workspace := req.Workspace
		if workspace == "" {
			workspace = d.state.Workspace
		}
		if !filepath.IsAbs(workspace) {
			workspace = filepath.Join(d.state.Workspace, workspace)
		}
		os.MkdirAll(workspace, 0o750)

		job := d.jobRunner.Submit(req.Prompt, workspace)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(job)

	case "GET":
		id := r.URL.Query().Get("id")
		if id != "" {
			job := d.jobRunner.Get(id)
			if job == nil {
				http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(job)
		} else {
			// List recent jobs.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(d.jobRunner.ListRecent(20))
		}

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// ---------- GitHub webhook handler -------------------------------------------

type ghWebhookPayload struct {
	Action string `json:"action"`
	// PR fields
	PullRequest *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"html_url"`
		Body   string `json:"body"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	// Issue fields
	Issue *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"html_url"`
		Body   string `json:"body"`
	} `json:"issue"`
	// Push fields
	Ref     string `json:"ref"`
	Compare string `json:"compare"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
	// Repo
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
}

func (d *dashboardServer) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify webhook signature if GITHUB_WEBHOOK_SECRET is set.
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !verifyGitHubSignature(body, sig, secret) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	} else if !apiAuth(r) {
		// If no webhook secret, require API token.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload ghWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	prompt := buildWebhookPrompt(event, &payload)
	if prompt == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored"}`))
		return
	}

	if d.bot == nil || d.jobRunner == nil {
		http.Error(w, `{"error":"bot not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Clone the repo into a temp workspace for the webhook run.
	workspace := filepath.Join(d.state.Workspace, "webhook", payload.Repository.FullName)
	os.MkdirAll(workspace, 0o750)

	job := d.jobRunner.Submit(prompt, workspace)

	log.Info().
		Str("event", event).
		Str("repo", payload.Repository.FullName).
		Str("job_id", job.ID).
		Msg("github webhook triggered")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(job)
}

func buildWebhookPrompt(event string, p *ghWebhookPayload) string {
	switch event {
	case "pull_request":
		if p.PullRequest == nil {
			return ""
		}
		switch p.Action {
		case "opened", "synchronize":
			return fmt.Sprintf(
				"A pull request was %s in %s.\nPR #%d: %s\nURL: %s\nBranch: %s\n\nDescription:\n%s\n\nPlease review this PR. Check for bugs, security issues, and suggest improvements. Be concise.",
				p.Action, p.Repository.FullName, p.PullRequest.Number, p.PullRequest.Title,
				p.PullRequest.URL, p.PullRequest.Head.Ref, truncate(p.PullRequest.Body, 500),
			)
		default:
			return ""
		}
	case "issues":
		if p.Issue == nil {
			return ""
		}
		if p.Action == "opened" {
			return fmt.Sprintf(
				"A new issue was opened in %s.\nIssue #%d: %s\nURL: %s\n\nDescription:\n%s\n\nPlease analyze this issue and suggest a fix approach. Be concise.",
				p.Repository.FullName, p.Issue.Number, p.Issue.Title,
				p.Issue.URL, truncate(p.Issue.Body, 500),
			)
		}
		return ""
	case "push":
		if !strings.HasSuffix(p.Ref, "/main") && !strings.HasSuffix(p.Ref, "/master") {
			return ""
		}
		var msgs []string
		for _, c := range p.Commits {
			msgs = append(msgs, c.Message)
		}
		return fmt.Sprintf(
			"New push to %s in %s.\nCommits:\n%s\nCompare: %s\n\nBriefly summarize what changed.",
			p.Ref, p.Repository.FullName, strings.Join(msgs, "\n"), p.Compare,
		)
	default:
		return ""
	}
}

func verifyGitHubSignature(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
