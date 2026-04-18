package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Job represents a scheduled recurring Claude Code task.
type Job struct {
	ID      int    `json:"id"`
	UserID  int64  `json:"user_id"`
	ChatID  int64  `json:"chat_id"`
	Cron    string `json:"cron"`    // simplified: "HH:MM" (daily) or "*/N" (every N minutes)
	Prompt  string `json:"prompt"`  // the Claude prompt to run
	Created string `json:"created"` // RFC3339
}

// Scheduler runs recurring Claude Code tasks on a schedule.
type Scheduler struct {
	mu       sync.Mutex
	jobs     []Job
	nextID   int
	filePath string
	bot      *Bot
}

func NewScheduler(filePath string, bot *Bot) *Scheduler {
	s := &Scheduler{
		filePath: filePath,
		bot:      bot,
		nextID:   1,
	}
	s.load()
	return s
}

func (s *Scheduler) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var jobs []Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		log.Warn().Err(err).Msg("scheduler: failed to load jobs")
		return
	}
	s.jobs = jobs
	for _, j := range jobs {
		if j.ID >= s.nextID {
			s.nextID = j.ID + 1
		}
	}
	log.Info().Int("count", len(jobs)).Msg("scheduler: loaded jobs")
}

func (s *Scheduler) save() {
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("scheduler: failed to marshal jobs")
		return
	}
	if err := os.WriteFile(s.filePath, data, 0o600); err != nil {
		log.Error().Err(err).Msg("scheduler: failed to save jobs")
	}
}

// Add creates a new scheduled job and persists it.
func (s *Scheduler) Add(uid, chatID int64, cron, prompt string) (Job, error) {
	if err := validateCron(cron); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	job := Job{
		ID:      s.nextID,
		UserID:  uid,
		ChatID:  chatID,
		Cron:    cron,
		Prompt:  prompt,
		Created: time.Now().Format(time.RFC3339),
	}
	s.nextID++
	s.jobs = append(s.jobs, job)
	s.save()
	return job, nil
}

// Cancel removes a job by ID. Returns true if found.
func (s *Scheduler) Cancel(uid int64, jobID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, j := range s.jobs {
		if j.ID == jobID && j.UserID == uid {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

// List returns all jobs for a user.
func (s *Scheduler) List(uid int64) []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Job
	for _, j := range s.jobs {
		if j.UserID == uid {
			out = append(out, j)
		}
	}
	return out
}

// Run starts the scheduler loop. It checks every minute.
func (s *Scheduler) Run(ctx context.Context) {
	log.Info().Msg("scheduler: started")
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	snapshot := make([]Job, len(s.jobs))
	copy(snapshot, s.jobs)
	s.mu.Unlock()

	for _, job := range snapshot {
		if shouldRun(job.Cron, now) {
			go s.execute(ctx, job)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, job Job) {
	log.Info().Int("job_id", job.ID).Int64("uid", job.UserID).Str("cron", job.Cron).Msg("scheduler: executing job")

	sess := s.bot.state.Session(job.UserID)

	if !sess.mu.TryLock() {
		log.Warn().Int("job_id", job.ID).Msg("scheduler: user session busy, skipping")
		return
	}
	defer sess.mu.Unlock()

	s.bot.state.Record(job.UserID, "in", fmt.Sprintf("[cron #%d] %s", job.ID, job.Prompt))

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	reply, err := runClaude(callCtx, sess, s.bot.model, job.Prompt)
	if err != nil {
		log.Error().Err(err).Int("job_id", job.ID).Msg("scheduler: claude failed")
		reply = "❌ " + err.Error()
		s.bot.state.Record(job.UserID, "error", reply)
	} else {
		s.bot.state.Record(job.UserID, "out", reply)
	}

	header := fmt.Sprintf("⏰ Scheduled job #%d:\n%s\n\n", job.ID, job.Prompt)
	fullReply := header + reply

	for _, chunk := range chunks(fullReply, maxTelegramMessage) {
		if err := s.bot.send(ctx, job.ChatID, chunk); err != nil {
			log.Warn().Err(err).Int("job_id", job.ID).Msg("scheduler: send failed")
			return
		}
	}
}

// validateCron checks if a cron expression is valid.
// Supported formats:
//   - "HH:MM" — runs daily at that time (UTC)
//   - "*/N"   — runs every N minutes
func validateCron(expr string) error {
	if strings.HasPrefix(expr, "*/") {
		n, err := strconv.Atoi(strings.TrimPrefix(expr, "*/"))
		if err != nil || n < 1 || n > 1440 {
			return fmt.Errorf("invalid interval: use */N where N is 1-1440 minutes")
		}
		return nil
	}
	parts := strings.SplitN(expr, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid schedule: use HH:MM (daily UTC) or */N (every N minutes)")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return fmt.Errorf("invalid hour: must be 0-23")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return fmt.Errorf("invalid minute: must be 0-59")
	}
	return nil
}

// shouldRun checks if a job should run at the given time.
func shouldRun(expr string, now time.Time) bool {
	now = now.UTC()
	if strings.HasPrefix(expr, "*/") {
		n, _ := strconv.Atoi(strings.TrimPrefix(expr, "*/"))
		minuteOfDay := now.Hour()*60 + now.Minute()
		return minuteOfDay%n == 0
	}
	parts := strings.SplitN(expr, ":", 2)
	if len(parts) != 2 {
		return false
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return now.Hour() == h && now.Minute() == m
}
