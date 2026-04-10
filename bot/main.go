package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	allowed := parseIDs(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"))
	if len(allowed) == 0 {
		log.Printf("warning: TELEGRAM_ALLOWED_USER_IDS is empty — bot will refuse all users")
	}

	model := strings.TrimSpace(os.Getenv("CLAUDE_MODEL"))
	workspace := envOr("CLAW_WORKSPACE", "/workspace")
	botName := envOr("BOT_NAME", "clawdy")
	port := envOr("DASHBOARD_PORT", "8080")
	password := strings.TrimSpace(os.Getenv("DASHBOARD_PASSWORD"))
	if password == "" {
		log.Printf("warning: DASHBOARD_PASSWORD is empty — login will be disabled")
	}

	if err := os.MkdirAll(workspace, 0o750); err != nil {
		log.Fatalf("mkdir workspace: %v", err)
	}

	state := NewState(botName, model, workspace, allowed)

	// Tee log output to the in-memory ring buffer so the dashboard can show
	// recent logs without tailing files.
	log.SetOutput(&teeWriter{ring: state, out: os.Stderr})
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Printf("openclaw starting — bot=%s allowed=%v workspace=%s dashboard=:%s",
		botName, allowed, workspace, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT/SIGTERM.
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigC
		log.Printf("signal %s received, shutting down", s)
		cancel()
	}()

	// HTTP dashboard.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           NewDashboard(state, password),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Printf("dashboard listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
			cancel()
		}
	}()

	bot := NewBot(token, state, model)
	go func() {
		defer wg.Done()
		if err := bot.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("bot exited: %v", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	wg.Wait()
	log.Printf("bye")
}

func parseIDs(raw string) []int64 {
	var out []int64
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// teeWriter sends each log line to both the in-memory ring buffer and a real
// writer (stderr in production). It splits on newlines so multi-line records
// still land as separate ring entries.
type teeWriter struct {
	ring *State
	out  *os.File
}

func (t *teeWriter) Write(p []byte) (int, error) {
	s := strings.TrimRight(string(p), "\n")
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			t.ring.Log(line)
		}
	}
	return t.out.Write(p)
}
