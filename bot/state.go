package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is a single in/out/error row shown on the dashboard.
type Event struct {
	Time      time.Time
	UserID    int64
	Direction string // "in" | "out" | "error"
	Text      string
}

// Session is a per-user Claude Code conversation. The mutex serializes claude
// calls for a single user so we don't interleave prompts.
type Session struct {
	UserID    int64
	SessionID string
	Cwd       string
	mu        sync.Mutex
}

// State is shared between the Telegram bot goroutine and the dashboard HTTP
// handlers. All accesses must go through the methods below.
type State struct {
	StartTime time.Time
	BotName   string
	Model     string
	Allowed   []int64
	Workspace string

	mu       sync.RWMutex
	sessions map[int64]*Session
	events   []Event
	logs     []string

	maxEvents int
	maxLogs   int
}

func NewState(botName, model, workspace string, allowed []int64) *State {
	return &State{
		StartTime: time.Now(),
		BotName:   botName,
		Model:     model,
		Allowed:   allowed,
		Workspace: workspace,
		sessions:  make(map[int64]*Session),
		maxEvents: 200,
		maxLogs:   300,
	}
}

// Session returns (or creates) the session for a given user id. Each user gets
// their own workspace subdir.
func (s *State) Session(uid int64) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[uid]; ok {
		return sess
	}
	cwd := filepath.Join(s.Workspace, strconv.FormatInt(uid, 10))
	_ = os.MkdirAll(cwd, 0o750)
	sess := &Session{UserID: uid, Cwd: cwd}
	s.sessions[uid] = sess
	return sess
}

func (s *State) Record(uid int64, dir, text string) {
	snippet := strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	if len(snippet) > 160 {
		snippet = snippet[:157] + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append([]Event{{time.Now(), uid, dir, snippet}}, s.events...)
	if len(s.events) > s.maxEvents {
		s.events = s.events[:s.maxEvents]
	}
}

func (s *State) Events() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// SessionsSnapshot returns a defensive copy that's safe to render from.
func (s *State) SessionsSnapshot() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0, len(s.sessions))
	for _, v := range s.sessions {
		out = append(out, Session{
			UserID:    v.UserID,
			SessionID: v.SessionID,
			Cwd:       v.Cwd,
		})
	}
	return out
}

func (s *State) IsAllowed(uid int64) bool {
	for _, a := range s.Allowed {
		if a == uid {
			return true
		}
	}
	return false
}

func (s *State) Log(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append([]string{line}, s.logs...)
	if len(s.logs) > s.maxLogs {
		s.logs = s.logs[:s.maxLogs]
	}
}

func (s *State) Logs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}
