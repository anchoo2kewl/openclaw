package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

const defaultUsersFile = "/etc/openclaw/users.json"

func main() {
	// ---- Subcommand router ------------------------------------------------
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "useradd":
			exitOnErr(cmdUserAdd(os.Args[2:]))
			return
		case "userdel":
			exitOnErr(cmdUserDel(os.Args[2:]))
			return
		case "userlist", "users":
			exitOnErr(cmdUserList(os.Args[2:]))
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}
	runServer()
}

func printUsage() {
	fmt.Println(`openclaw — Telegram-driven Claude Code

Usage:
  openclaw                    run the bot + dashboard (default)
  openclaw useradd EMAIL      provision a dashboard login (prompts for password)
  openclaw userdel EMAIL      remove a dashboard login
  openclaw userlist           list dashboard logins

Environment:
  TELEGRAM_BOT_TOKEN          telegram bot token
  TELEGRAM_ALLOWED_USER_IDS   comma-separated numeric telegram user ids
  CLAUDE_MODEL                optional model override
  CLAW_WORKSPACE              workspace root (default /workspace)
  BOT_NAME                    dashboard display name (default clawdy)
  DASHBOARD_PORT              http port (default 8080)
  USERS_FILE                  path to users.json (default ` + defaultUsersFile + `)
  DASHBOARD_PASSWORD          (bootstrap only) password for admin@openclaw.local
                              if the users file is empty on first run`)
}

// ---- server mode ----------------------------------------------------------

func runServer() {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		fmt.Fprintln(os.Stderr, "TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	allowed := parseIDs(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"))
	model := strings.TrimSpace(os.Getenv("CLAUDE_MODEL"))
	workspace := envOr("CLAW_WORKSPACE", "/workspace")
	botName := envOr("BOT_NAME", "clawdy")
	port := envOr("DASHBOARD_PORT", "8080")
	usersFile := envOr("USERS_FILE", defaultUsersFile)

	if err := os.MkdirAll(workspace, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir workspace: %v\n", err)
		os.Exit(1)
	}

	state := NewState(botName, model, workspace, allowed)
	initLogging(state)

	users, err := NewUserStore(usersFile)
	if err != nil {
		log.Fatal().Err(err).Str("path", usersFile).Msg("load users file")
	}

	// Bootstrap a default admin if the store is empty and DASHBOARD_PASSWORD
	// is set — this keeps existing deployments working after the schema
	// change without manual intervention.
	if users.Empty() {
		if bootstrap := strings.TrimSpace(os.Getenv("DASHBOARD_PASSWORD")); bootstrap != "" {
			if err := users.Add("admin@openclaw.local", "admin", bootstrap); err != nil {
				log.Fatal().Err(err).Msg("bootstrap admin user")
			}
			log.Warn().
				Str("email", "admin@openclaw.local").
				Msg("bootstrapped admin account from DASHBOARD_PASSWORD — run `openclaw useradd` to provision real logins")
		} else {
			log.Warn().Msg("no dashboard logins provisioned — login will be rejected until `openclaw useradd` has been run")
		}
	}

	if len(allowed) == 0 {
		log.Warn().Msg("TELEGRAM_ALLOWED_USER_IDS is empty — bot will silently drop all messages")
	}

	gatewayURL := strings.TrimSpace(os.Getenv("GATEWAY_URL"))
	gatewayToken := strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN"))
	hermesURL := strings.TrimSpace(os.Getenv("HERMES_URL"))

	log.Info().
		Str("bot", botName).
		Ints64("telegram_allowed", allowed).
		Str("workspace", workspace).
		Str("users_file", usersFile).
		Int("dashboard_accounts", len(users.List())).
		Str("dashboard_port", port).
		Bool("gateway_proxy", gatewayURL != "").
		Bool("hermes_proxy", hermesURL != "").
		Msg("openclaw starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigC
		log.Info().Str("signal", s.String()).Msg("shutdown requested")
		cancel()
	}()

	bot := NewBot(token, state, model)

	projectsFile := filepath.Join(workspace, ".claw-projects.json")
	bot.projects = NewProjectStore(projectsFile)

	claudeHome := envOr("CLAUDE_HOME", filepath.Join(os.Getenv("HOME"), ".claude"))
	bot.tools = NewToolManager(claudeHome)
	bot.history = NewHistoryStore(workspace)

	pluginDir := filepath.Join(workspace, ".claw-plugins")
	bot.plugins = NewPluginStore(pluginDir, bot.tools)

	jobsFile := filepath.Join(workspace, ".claw-jobs.json")
	scheduler := NewScheduler(jobsFile, bot)
	bot.scheduler = scheduler

	srv := &http.Server{
		Addr: ":" + port,
		Handler: NewDashboard(state, DashboardConfig{
			Users:        users,
			GatewayURL:   gatewayURL,
			GatewayToken: gatewayToken,
			HermesURL:    hermesURL,
			Bot:          bot,
		}),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		log.Info().Str("addr", srv.Addr).Msg("dashboard listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("dashboard server exited")
			cancel()
		}
	}()

	go func() {
		defer wg.Done()
		scheduler.Run(ctx)
	}()

	go func() {
		defer wg.Done()
		if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("telegram bot exited")
			cancel()
		}
	}()

	<-ctx.Done()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	wg.Wait()
	log.Info().Msg("bye")
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

// ---- user management subcommands -----------------------------------------

func cmdUserAdd(args []string) error {
	// Usage: openclaw useradd EMAIL [USERNAME]
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: openclaw useradd EMAIL [USERNAME]")
	}
	email := args[0]
	username := ""
	if len(args) == 2 {
		username = args[1]
	}
	usersFile := envOr("USERS_FILE", defaultUsersFile)

	users, err := NewUserStore(usersFile)
	if err != nil {
		return fmt.Errorf("load users: %w", err)
	}

	pw, err := readPasswordTwice("Password: ", "Confirm:  ")
	if err != nil {
		return err
	}
	if err := users.Add(email, username, pw); err != nil {
		return err
	}
	displayUser := username
	if displayUser == "" {
		displayUser = usernameFromEmail(email)
	}
	fmt.Printf("ok — %s (username: %s) provisioned (users file: %s)\n", email, displayUser, usersFile)
	return nil
}

func cmdUserDel(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: openclaw userdel EMAIL")
	}
	email := args[0]
	usersFile := envOr("USERS_FILE", defaultUsersFile)

	users, err := NewUserStore(usersFile)
	if err != nil {
		return err
	}
	if err := users.Delete(email); err != nil {
		return err
	}
	fmt.Printf("ok — %s removed\n", email)
	return nil
}

func cmdUserList(_ []string) error {
	usersFile := envOr("USERS_FILE", defaultUsersFile)
	users, err := NewUserStore(usersFile)
	if err != nil {
		return err
	}
	list := users.List()
	if len(list) == 0 {
		fmt.Println("(no accounts)")
		return nil
	}
	for _, r := range list {
		fmt.Printf("%-20s %s\n", r.Username, r.Email)
	}
	return nil
}

func readPasswordTwice(p1, p2 string) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, p1)
		pw1, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, p2)
		pw2, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(pw1) != string(pw2) {
			return "", errors.New("passwords do not match")
		}
		if len(pw1) < 8 {
			return "", errors.New("password must be at least 8 characters")
		}
		return string(pw1), nil
	}
	// Non-interactive: read a single line from stdin (useful for
	// scripts: echo "s3cret" | openclaw useradd alice@foo).
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && err.Error() != "EOF" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "openclaw: %v\n", err)
		os.Exit(1)
	}
}
