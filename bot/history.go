package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HistoryEntry is a single persisted conversation line.
type HistoryEntry struct {
	Time      string `json:"time"`
	UserID    int64  `json:"user_id"`
	Direction string `json:"direction"` // "in" | "out" | "error"
	Text      string `json:"text"`
}

// HistoryStore persists conversation history as JSON Lines files.
// One file per user: <workspace>/.claw-history-<uid>.jsonl
type HistoryStore struct {
	mu        sync.Mutex
	workspace string
}

func NewHistoryStore(workspace string) *HistoryStore {
	return &HistoryStore{workspace: workspace}
}

func (h *HistoryStore) filePath(uid int64) string {
	return filepath.Join(h.workspace, fmt.Sprintf(".claw-history-%d.jsonl", uid))
}

// Append writes a new entry to the user's history file.
func (h *HistoryStore) Append(uid int64, direction, text string) {
	entry := HistoryEntry{
		Time:      time.Now().Format(time.RFC3339),
		UserID:    uid,
		Direction: direction,
		Text:      text,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	f, err := os.OpenFile(h.filePath(uid), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

// Recent returns the last N entries for a user.
func (h *HistoryStore) Recent(uid int64, n int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	lines := readLastLines(h.filePath(uid), n)
	entries := make([]HistoryEntry, 0, len(lines))
	for _, line := range lines {
		var e HistoryEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// Search returns entries matching a query string (case-insensitive).
func (h *HistoryStore) Search(uid int64, query string, maxResults int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	query = strings.ToLower(query)
	var results []HistoryEntry

	f, err := os.Open(h.filePath(uid))
	if err != nil {
		return results
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		var e HistoryEntry
		if json.Unmarshal(scanner.Bytes(), &e) == nil {
			if strings.Contains(strings.ToLower(e.Text), query) {
				results = append(results, e)
			}
		}
	}

	// Return only the last maxResults matches.
	if len(results) > maxResults {
		results = results[len(results)-maxResults:]
	}
	return results
}

// SearchAll searches across all users' history files. For dashboard use.
func (h *HistoryStore) SearchAll(query string, maxResults int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	query = strings.ToLower(query)
	var results []HistoryEntry

	entries, _ := os.ReadDir(h.workspace)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".claw-history-") || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(h.workspace, e.Name()))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 256*1024)
		for scanner.Scan() {
			var entry HistoryEntry
			if json.Unmarshal(scanner.Bytes(), &entry) == nil {
				if strings.Contains(strings.ToLower(entry.Text), query) {
					results = append(results, entry)
				}
			}
		}
		f.Close()
	}

	if len(results) > maxResults {
		results = results[len(results)-maxResults:]
	}
	return results
}

// FormatEntries formats history entries for Telegram display.
func FormatEntries(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "No history found."
	}
	var sb strings.Builder
	for _, e := range entries {
		t, _ := time.Parse(time.RFC3339, e.Time)
		arrow := "→"
		if e.Direction == "out" {
			arrow = "←"
		} else if e.Direction == "error" {
			arrow = "⚠"
		}
		text := e.Text
		if len(text) > 120 {
			text = text[:117] + "..."
		}
		fmt.Fprintf(&sb, "%s %s %s\n", t.Format("Jan 02 15:04"), arrow, text)
	}
	return sb.String()
}

// readLastLines reads the last n lines from a file efficiently.
func readLastLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// FormatHistoryForDashboard returns JSON-ready history entries for a user ID string.
func (h *HistoryStore) ForUserID(uidStr string, n int) []HistoryEntry {
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return nil
	}
	return h.Recent(uid, n)
}
