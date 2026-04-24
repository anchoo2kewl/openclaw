package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ---------- Brief Scheduler --------------------------------------------------

type BriefScheduler struct {
	store    *BriefStore
	botToken string
}

func NewBriefScheduler(store *BriefStore, botToken string) *BriefScheduler {
	return &BriefScheduler{store: store, botToken: botToken}
}

func (bs *BriefScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	log.Info().Msg("brief scheduler started")
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("brief scheduler stopped")
			return
		case t := <-ticker.C:
			bs.tick(ctx, t)
		}
	}
}

func (bs *BriefScheduler) tick(ctx context.Context, now time.Time) {
	utc := now.UTC()
	for _, b := range bs.store.List() {
		if !b.Enabled {
			continue
		}
		if !cronMatches(b.CronExpr, utc) {
			continue
		}
		go bs.runBrief(ctx, b)
	}
}

func (bs *BriefScheduler) runBrief(ctx context.Context, b Brief) {
	log.Info().Str("brief", b.ID).Str("type", string(b.Type)).Msg("executing brief")

	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	msg, err := executeBrief(runCtx, b)
	if err != nil {
		log.Error().Err(err).Str("brief", b.ID).Msg("brief execution failed")
		bs.store.RecordRun(b.ID, "error", err.Error())
		return
	}

	if b.ChatID == "" || bs.botToken == "" {
		log.Warn().Str("brief", b.ID).Str("chat_id", b.ChatID).Msg("brief skipped delivery: no chat_id or bot token")
		bs.store.RecordRun(b.ID, "error", "no chat_id configured — edit the brief and set a Telegram chat ID")
		return
	}

	if err := sendBriefToTelegram(runCtx, bs.botToken, b.ChatID, msg); err != nil {
		log.Error().Err(err).Str("brief", b.ID).Msg("brief telegram delivery failed")
		bs.store.RecordRun(b.ID, "error", "delivery: "+err.Error())
		return
	}

	log.Info().Str("brief", b.ID).Msg("brief completed")
	bs.store.RecordRun(b.ID, "ok", "")
}

// RunNow executes a brief immediately (for the "Run Now" button).
func (bs *BriefScheduler) RunNow(ctx context.Context, id string) error {
	b, ok := bs.store.Get(id)
	if !ok {
		return nil
	}
	go bs.runBrief(ctx, b)
	return nil
}

// ---------- 5-field Cron Matcher ---------------------------------------------
// Supports: *, exact (30), ranges (1-5), lists (1,3,5), steps (*/15)

func cronMatches(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	minute := t.Minute()
	hour := t.Hour()
	dom := t.Day()
	month := int(t.Month())
	dow := int(t.Weekday()) // 0=Sunday

	return fieldMatches(fields[0], minute, 0, 59) &&
		fieldMatches(fields[1], hour, 0, 23) &&
		fieldMatches(fields[2], dom, 1, 31) &&
		fieldMatches(fields[3], month, 1, 12) &&
		fieldMatches(fields[4], dow, 0, 7) // 0 and 7 both mean Sunday
}

func fieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		if partMatches(part, value, min, max) {
			return true
		}
	}
	return false
}

func partMatches(part string, value, min, max int) bool {
	part = strings.TrimSpace(part)

	// Handle step: */N or range/N
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return false
		}
		step = s
		part = part[:idx]
	}

	// Wildcard
	if part == "*" {
		if step == 1 {
			return true
		}
		return (value-min)%step == 0
	}

	// Range: N-M
	if idx := strings.Index(part, "-"); idx >= 0 {
		lo, err1 := strconv.Atoi(part[:idx])
		hi, err2 := strconv.Atoi(part[idx+1:])
		if err1 != nil || err2 != nil {
			return false
		}
		// Handle Sunday as 7 → treat as 0
		if max == 7 {
			if lo == 7 {
				lo = 0
			}
			if hi == 7 {
				hi = 0
			}
			if value == 7 {
				value = 0
			}
		}
		if value < lo || value > hi {
			return false
		}
		return (value-lo)%step == 0
	}

	// Exact value
	n, err := strconv.Atoi(part)
	if err != nil {
		return false
	}
	// Sunday normalization for day-of-week
	if max == 7 && n == 7 {
		n = 0
	}
	if max == 7 && value == 7 {
		value = 0
	}
	return value == n
}
