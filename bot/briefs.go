package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------- Types ------------------------------------------------------------

type BriefType string

const (
	BriefHackerNews BriefType = "hackernews"
	BriefWorldNews  BriefType = "worldnews"
	BriefWeather    BriefType = "weather"
	BriefStocks     BriefType = "stocks"
)

type TickerEntry struct {
	Symbol string `json:"symbol"`
	Label  string `json:"label"`
}

type BriefConfig struct {
	Location string        `json:"location,omitempty"`
	Tickers  []TickerEntry `json:"tickers,omitempty"`
	Count    int           `json:"count,omitempty"`
}

type Brief struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        BriefType   `json:"type"`
	Enabled     bool        `json:"enabled"`
	CronExpr    string      `json:"cron_expr"`
	CronDisplay string      `json:"cron_display"`
	Config      BriefConfig `json:"config"`
	ChatID      string      `json:"chat_id"`
	LastRunAt   *time.Time  `json:"last_run_at"`
	LastStatus  string      `json:"last_status"`
	LastError   string      `json:"last_error,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// ---------- Store ------------------------------------------------------------

type BriefStore struct {
	mu       sync.RWMutex
	briefs   []Brief
	filePath string
}

func NewBriefStore(filePath, defaultChatID string) *BriefStore {
	s := &BriefStore{filePath: filePath}
	if err := s.load(); err != nil || len(s.briefs) == 0 {
		s.briefs = defaultBriefs(defaultChatID)
		_ = s.save()
	}
	return s
}

func (s *BriefStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.briefs)
}

func (s *BriefStore) save() error {
	data, err := json.MarshalIndent(s.briefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o600)
}

func (s *BriefStore) List() []Brief {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Brief, len(s.briefs))
	copy(out, s.briefs)
	return out
}

func (s *BriefStore) Get(id string) (Brief, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, b := range s.briefs {
		if b.ID == id {
			return b, true
		}
	}
	return Brief{}, false
}

func (s *BriefStore) Create(b Brief) (Brief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b.ID == "" {
		b.ID = slugify(b.Name)
	}
	for _, existing := range s.briefs {
		if existing.ID == b.ID {
			return Brief{}, fmt.Errorf("brief %q already exists", b.ID)
		}
	}
	now := time.Now()
	b.CreatedAt = now
	b.UpdatedAt = now
	s.briefs = append(s.briefs, b)
	return b, s.save()
}

func (s *BriefStore) Update(id string, patch Brief) (Brief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, b := range s.briefs {
		if b.ID == id {
			if patch.Name != "" {
				s.briefs[i].Name = patch.Name
			}
			if patch.CronExpr != "" {
				s.briefs[i].CronExpr = patch.CronExpr
			}
			if patch.CronDisplay != "" {
				s.briefs[i].CronDisplay = patch.CronDisplay
			}
			if patch.ChatID != "" {
				s.briefs[i].ChatID = patch.ChatID
			}
			if patch.Config.Location != "" {
				s.briefs[i].Config.Location = patch.Config.Location
			}
			if patch.Config.Tickers != nil {
				s.briefs[i].Config.Tickers = patch.Config.Tickers
			}
			if patch.Config.Count > 0 {
				s.briefs[i].Config.Count = patch.Config.Count
			}
			s.briefs[i].UpdatedAt = time.Now()
			return s.briefs[i], s.save()
		}
	}
	return Brief{}, fmt.Errorf("brief %q not found", id)
}

func (s *BriefStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, b := range s.briefs {
		if b.ID == id {
			s.briefs = append(s.briefs[:i], s.briefs[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("brief %q not found", id)
}

func (s *BriefStore) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, b := range s.briefs {
		if b.ID == id {
			s.briefs[i].Enabled = enabled
			s.briefs[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("brief %q not found", id)
}

func (s *BriefStore) RecordRun(id, status, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, b := range s.briefs {
		if b.ID == id {
			now := time.Now()
			s.briefs[i].LastRunAt = &now
			s.briefs[i].LastStatus = status
			s.briefs[i].LastError = errMsg
			_ = s.save()
			return
		}
	}
}

// ---------- Helpers ----------------------------------------------------------

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	var out []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return fmt.Sprintf("brief-%d", time.Now().UnixMilli())
	}
	return string(out)
}

func defaultBriefs(chatID string) []Brief {
	now := time.Now()
	return []Brief{
		{
			ID: "hn-daily-top-10", Name: "HN Daily Top 10",
			Type: BriefHackerNews, Enabled: true,
			CronExpr: "30 11 * * 1-5", CronDisplay: "weekdays 7:30 AM EDT",
			Config:  BriefConfig{Count: 10},
			ChatID:  chatID, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "world-news", Name: "World News Top 10",
			Type: BriefWorldNews, Enabled: true,
			CronExpr: "30 11 * * 1-5", CronDisplay: "weekdays 7:30 AM EDT",
			Config:  BriefConfig{Count: 10},
			ChatID:  chatID, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "weather-mississauga", Name: "Weather — Mississauga",
			Type: BriefWeather, Enabled: true,
			CronExpr: "30 11 * * 1-5", CronDisplay: "weekdays 7:30 AM EDT",
			Config:  BriefConfig{Location: "Mississauga,Ontario,Canada"},
			ChatID:  chatID, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "morning-market-brief", Name: "Morning Market Brief",
			Type: BriefStocks, Enabled: true,
			CronExpr: "30 11 * * 1-5", CronDisplay: "weekdays 7:30 AM EDT",
			Config: BriefConfig{
				Tickers: []TickerEntry{
					{"GOOGL", "Google (Alphabet)"},
					{"RDDT", "Reddit"},
					{"^GSPC", "S&P 500"},
					{"GC=F", "Gold (USD/oz)"},
					{"BTC-USD", "Bitcoin"},
					{"ETH-USD", "Ethereum"},
				},
			},
			ChatID: chatID, CreatedAt: now, UpdatedAt: now,
		},
	}
}
