package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

// ---------- Template model -------------------------------------------------

type fileEntry struct {
	Path string
	Size int64
}

type dashView struct {
	Bot        string
	Model      string
	Allowed    []int64
	Workspace  string
	Uptime     string
	Authed     bool
	Email      string // logged-in user, only set on authed view
	HasGateway bool
	Users      []UserRow
	Sessions   []Session
	Events     []Event
	Files      []fileEntry
	Logs       []string
	CSS        template.CSS
	Error      string // for login page only
}

// ---------- Helpers --------------------------------------------------------

func listWorkspace(root string) []fileEntry {
	var out []fileEntry
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, fileEntry{rel, info.Size()})
		if len(out) >= 100 {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func fmtSize(n int64) string {
	const unit int64 = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := unit, 0
	for nn := n / unit; nn >= unit; nn /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%c", float64(n)/float64(div), "KMGT"[exp])
}

func fmtUptime(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}

// ---------- HTML templates -------------------------------------------------

const dashboardCSS = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", ui-sans-serif, system-ui, sans-serif; background: #0b0d10; color: #e6e9ef; max-width: 960px; margin: 0 auto; line-height: 1.45; }
.top { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
h1 { margin: 0 0 4px 0; font-size: 22px; letter-spacing: -0.01em; }
h2 { margin: 28px 0 8px; font-size: 13px; text-transform: uppercase; letter-spacing: 0.08em; color: #8b94a8; font-weight: 600; }
.sub { color: #8b94a8; font-size: 13px; margin-bottom: 20px; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #34d399; margin-right: 6px; vertical-align: middle; }
.card { background: #141820; border: 1px solid #1f2632; border-radius: 10px; padding: 14px 16px; margin: 6px 0; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
.grid .card .k { font-size: 11px; color: #8b94a8; text-transform: uppercase; letter-spacing: 0.06em; }
.grid .card .v { font-size: 16px; margin-top: 4px; font-variant-numeric: tabular-nums; word-break: break-all; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
td, th { padding: 6px 8px; text-align: left; border-bottom: 1px solid #1f2632; vertical-align: top; }
th { font-size: 11px; color: #8b94a8; text-transform: uppercase; letter-spacing: 0.06em; font-weight: 600; }
.dir-in { color: #60a5fa; }
.dir-out { color: #34d399; }
.dir-error { color: #f87171; }
pre { background: #0f131a; border: 1px solid #1f2632; border-radius: 8px; padding: 12px; overflow-x: auto; font-size: 12px; line-height: 1.5; color: #c8d0dd; margin: 6px 0; max-height: 320px; }
code { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, monospace; }
a { color: #60a5fa; text-decoration: none; }
a:hover { text-decoration: underline; }
.muted { color: #8b94a8; }
.foot { margin-top: 32px; padding-top: 14px; border-top: 1px solid #1f2632; font-size: 12px; color: #8b94a8; display: flex; justify-content: space-between; }
.btn { display: inline-block; padding: 7px 14px; border-radius: 8px; background: #1f2632; color: #e6e9ef; border: 1px solid #2a3444; font-size: 13px; font-weight: 500; cursor: pointer; text-decoration: none; }
.btn:hover { background: #2a3444; text-decoration: none; }
.btn-primary { background: #2563eb; border-color: #2563eb; }
.btn-primary:hover { background: #1d4ed8; }
form.login { max-width: 340px; margin: 80px auto 0; padding: 28px; background: #141820; border: 1px solid #1f2632; border-radius: 14px; }
form.login h1 { margin-bottom: 18px; }
form.login label { display: block; font-size: 12px; color: #8b94a8; text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 6px; }
form.login input { width: 100%; padding: 10px 12px; background: #0b0d10; color: #e6e9ef; border: 1px solid #2a3444; border-radius: 8px; font-size: 14px; margin-bottom: 16px; font-family: inherit; box-sizing: border-box; -webkit-appearance: none; appearance: none; }
form.login input:focus { outline: none; border-color: #2563eb; }
/* Kill Chrome's yellow autofill background — paint the form field with
   the same dark treatment by using a huge inset box-shadow instead. */
form.login input:-webkit-autofill,
form.login input:-webkit-autofill:hover,
form.login input:-webkit-autofill:focus,
form.login input:-webkit-autofill:active {
  -webkit-box-shadow: 0 0 0 1000px #0b0d10 inset !important;
  -webkit-text-fill-color: #e6e9ef !important;
  caret-color: #e6e9ef;
  transition: background-color 9999s ease-in-out 0s;
}
form.login button { width: 100%; padding: 10px; }
.err { color: #f87171; font-size: 13px; margin: -8px 0 12px; }
.lede { font-size: 15px; color: #c8d0dd; }
.features { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }
.features .card { padding: 16px 18px; }
.feat { font-weight: 600; font-size: 14px; margin-bottom: 4px; color: #e6e9ef; }
`

// The public landing page is intentionally generic — no uptime, no model,
// no bot name, no user counts. Attackers probing the domain should learn
// nothing about what's running here or how many users it has.
const publicHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw</title>
<style>{{.CSS}}</style>
</head><body>

<div class=top>
  <div>
    <h1>openclaw</h1>
    <div class=sub>Telegram-first autonomous coding.</div>
  </div>
  <div>
    <a class=btn href="/login">Sign in</a>
  </div>
</div>

<div class=card style="margin-top:18px">
  <div class=lede>
    Drive a sandboxed coding agent from anywhere over a simple chat interface.
    Private by default, hosted by you, owned by you.
  </div>
</div>

<h2>features</h2>
<div class=features>
  <div class=card>
    <div class=feat>Chat-driven workflows</div>
    <div class=muted>Send a message, get a result. Long-running agent loops stream progress back to you as they finish.</div>
  </div>
  <div class=card>
    <div class=feat>Private allowlist</div>
    <div class=muted>No signups, no public access. Only explicitly approved accounts can interact with the agent.</div>
  </div>
  <div class=card>
    <div class=feat>Resumable sessions</div>
    <div class=muted>Each account gets its own persistent conversation and a dedicated workspace on disk.</div>
  </div>
  <div class=card>
    <div class=feat>Single static binary</div>
    <div class=muted>Pure Go standard library. No framework sprawl, no runtime dependencies, easy to audit.</div>
  </div>
  <div class=card>
    <div class=feat>Container sandbox</div>
    <div class=muted>Commands run inside a disposable container with a scoped workspace volume.</div>
  </div>
  <div class=card>
    <div class=feat>Cookie-based auth</div>
    <div class=muted>Password-protected management console. No third-party identity provider required.</div>
  </div>
</div>

<div class=foot>
  <div>self-hosted · open source</div>
  <div><a href="https://github.com/anchoo2kewl/openclaw">github.com/anchoo2kewl/openclaw</a></div>
</div>
</body></html>`

// The authed dashboard is the operational view — everything sensitive lives
// here and nowhere else.
const dashboardHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<meta http-equiv=refresh content=10>
<title>openclaw · {{.Bot}}</title>
<style>{{.CSS}}</style>
</head><body>

<div class=top>
  <div>
    <h1>openclaw <span class=muted style="font-size:14px">/ {{.Bot}}</span></h1>
    <div class=sub><span class=dot></span>online · uptime {{.Uptime}} · {{len .Sessions}} active session(s)</div>
  </div>
  <div style="text-align:right">
    <div class=muted style="font-size:12px;margin-bottom:6px">{{.Email}}</div>
    <div style="display:flex;gap:8px;justify-content:flex-end">
      {{if .HasGateway}}<a class="btn btn-primary" href="/gateway-launch">Open gateway</a>{{end}}
      <form method=POST action="/logout" style="margin:0"><button class=btn type=submit>Log out</button></form>
    </div>
  </div>
</div>

<div class=grid>
  <div class=card><div class=k>model</div><div class=v>{{.Model}}</div></div>
  <div class=card><div class=k>allowed telegram users</div><div class=v>{{if .Allowed}}{{range $i, $u := .Allowed}}{{if $i}}, {{end}}{{$u}}{{end}}{{else}}(none){{end}}</div></div>
  <div class=card><div class=k>workspace</div><div class=v>{{.Workspace}}</div></div>
</div>

<h2>dashboard accounts</h2>
{{if .Users}}
<table><thead><tr><th>username</th><th>email</th></tr></thead><tbody>
{{range .Users}}<tr><td><code>{{.Username}}</code></td><td>{{.Email}}</td></tr>{{end}}
</tbody></table>
{{else}}<div class="card muted">no accounts provisioned</div>{{end}}

<h2>telegram sessions</h2>
{{if .Sessions}}
<table><thead><tr><th>user</th><th>session_id</th><th>cwd</th></tr></thead><tbody>
{{range .Sessions}}<tr><td>{{.UserID}}</td><td><code>{{if .SessionID}}{{.SessionID}}{{else}}—{{end}}</code></td><td><code>{{.Cwd}}</code></td></tr>{{end}}
</tbody></table>
{{else}}<div class="card muted">no active sessions yet</div>{{end}}

<h2>recent activity</h2>
{{if .Events}}
<table><thead><tr><th style="width:72px">time</th><th style="width:60px">dir</th><th style="width:100px">user</th><th>text</th></tr></thead><tbody>
{{range .Events}}<tr><td class=muted>{{fmtTime .Time}}</td><td class="dir-{{.Direction}}">{{.Direction}}</td><td>{{.UserID}}</td><td>{{.Text}}</td></tr>{{end}}
</tbody></table>
{{else}}<div class="card muted">no messages yet</div>{{end}}

<h2>workspace files</h2>
{{if .Files}}
<table><thead><tr><th>path</th><th style="width:80px;text-align:right">size</th></tr></thead><tbody>
{{range .Files}}<tr><td><code>{{.Path}}</code></td><td style="text-align:right" class=muted>{{fmtSize .Size}}</td></tr>{{end}}
</tbody></table>
{{else}}<div class="card muted">workspace is empty</div>{{end}}

<h2>logs (tail)</h2>
{{if .Logs}}<pre>{{range .Logs}}{{.}}
{{end}}</pre>{{else}}<div class="card muted">no logs yet</div>{{end}}

<div class=foot>
  <div>refreshes every 10s</div>
  <div><a href="https://github.com/anchoo2kewl/openclaw">github.com/anchoo2kewl/openclaw</a></div>
</div>
</body></html>`

const loginHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw · sign in</title>
<style>{{.CSS}}</style>
</head><body>
<form class=login method=POST action="/login">
  <h1>openclaw</h1>
  {{if .Error}}<div class=err>Invalid credentials</div>{{end}}
  <label for=identifier>Email or username</label>
  <input id=identifier name=identifier type=text autocomplete=username autofocus required>
  <label for=password>Password</label>
  <input id=password name=password type=password autocomplete=current-password required>
  <button class="btn btn-primary" type=submit>Sign in</button>
</form>
<div class=foot style="max-width:340px;margin:12px auto 0">
  <div><a href="/">&larr; back</a></div>
  <div><a href="https://github.com/anchoo2kewl/openclaw">github</a></div>
</div>
</body></html>`

var (
	dashTmpl = template.Must(template.New("dash").Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string { return t.Format("15:04:05") },
		"fmtSize": fmtSize,
	}).Parse(dashboardHTML))

	publicTmpl = template.Must(template.New("public").Parse(publicHTML))
	loginTmpl  = template.Must(template.New("login").Parse(loginHTML))
)

// ---------- HTTP handlers --------------------------------------------------

type dashboardServer struct {
	state       *State
	users       *UserStore
	sessions    *sessionStore
	gatewayURL  string
	hasGateway  bool
}

// DashboardConfig groups external wiring so main.go can plumb the gateway
// reverse proxy in without the caller of NewDashboard growing each time.
type DashboardConfig struct {
	Users        *UserStore
	GatewayURL   string // e.g. http://gateway:18789
	GatewayToken string // shared secret for gateway.auth.token
}

// NewDashboard builds the full HTTP handler tree. Public endpoints: /,
// /login, /logout, /health. /api/status and /gateway/ require auth.
func NewDashboard(s *State, cfg DashboardConfig) http.Handler {
	d := &dashboardServer{
		state:      s,
		users:      cfg.Users,
		sessions:   newSessionStore(12 * time.Hour),
		gatewayURL: cfg.GatewayURL,
		hasGateway: cfg.GatewayURL != "",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/login", d.handleLogin)
	mux.HandleFunc("/logout", d.handleLogout)
	mux.HandleFunc("/api/status", d.handleAPIStatus)

	if d.hasGateway {
		// Register BOTH /gateway and /gateway/ so the upstream JS, which
		// tries to open wss://claw.biswas.me/gateway (no trailing slash)
		// on the Control UI's default path, doesn't get a 301 that its
		// WebSocket client cannot follow. The proxy itself strips the
		// /gateway prefix before forwarding.
		gatewayHandler := newGatewayProxy(cfg.GatewayURL, cfg.GatewayToken, d.sessions)
		mux.Handle("/gateway/", gatewayHandler)
		mux.Handle("/gateway", gatewayHandler)
		// /gateway-launch: authed-only endpoint that redirects to
		// /gateway/#token=<token>. Modern browsers preserve the hash
		// fragment from Location: headers, so the upstream JS reads the
		// token straight out of window.location.hash without the user
		// ever seeing the WebSocket URL form.
		mux.HandleFunc("/gateway-launch", func(w http.ResponseWriter, r *http.Request) {
			if d.sessions.authedEmail(r) == "" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			target := "/gateway/"
			if cfg.GatewayToken != "" {
				target += "#token=" + cfg.GatewayToken
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
		})
	}

	mux.HandleFunc("/", d.handleIndex)
	return mux
}

func (d *dashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	setSecurityHeaders(w)
	if email := d.sessions.authedEmail(r); email != "" {
		d.renderAuthedDashboard(w, email)
		return
	}
	d.renderPublicLanding(w)
}

func (d *dashboardServer) renderPublicLanding(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = publicTmpl.Execute(w, dashView{CSS: template.CSS(dashboardCSS)})
}

func (d *dashboardServer) renderAuthedDashboard(w http.ResponseWriter, email string) {
	s := d.state
	sess := s.SessionsSnapshot()
	sort.Slice(sess, func(i, j int) bool { return sess[i].UserID < sess[j].UserID })

	view := dashView{
		Bot:        s.BotName,
		Model:      orDefault(s.Model, "(default)"),
		Allowed:    s.Allowed,
		Workspace:  s.Workspace,
		Uptime:     fmtUptime(time.Since(s.StartTime)),
		Authed:     true,
		Email:      email,
		HasGateway: d.hasGateway,
		Users:      d.users.List(),
		Sessions:   sess,
		Events:     s.Events(),
		Files:      listWorkspace(s.Workspace),
		Logs:       s.Logs(),
		CSS:        template.CSS(dashboardCSS),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashTmpl.Execute(w, view)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func (d *dashboardServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if email := d.sessions.authedEmail(r); email != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		d.renderLogin(w, "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			d.renderLogin(w, "invalid")
			return
		}
		identifier := r.PostFormValue("identifier")
		if identifier == "" {
			identifier = r.PostFormValue("email") // legacy field name
		}
		password := r.PostFormValue("password")

		canonical := d.users.Verify(identifier, password)
		if canonical == "" {
			// Small extra delay to blunt brute-force attempts; Verify
			// already burns PBKDF2 iterations regardless of which path it
			// took, so we don't leak "email exists" via timing here.
			time.Sleep(250 * time.Millisecond)
			log.Warn().Str("ip", clientIP(r)).Msg("login failed")
			d.renderLogin(w, "invalid")
			return
		}
		token := d.sessions.issue(canonical)
		if token == "" {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true, // Cloudflare terminates TLS in front of nginx
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int((12 * time.Hour).Seconds()),
		})
		log.Info().Str("email", canonical).Str("ip", clientIP(r)).Msg("login ok")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (d *dashboardServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		d.sessions.revoke(c.Value)
	}
	// Expire the cookie client-side too.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (d *dashboardServer) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if d.sessions.authedEmail(r) == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	s := d.state
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"bot":            s.BotName,
		"model":          s.Model,
		"uptime_seconds": int(time.Since(s.StartTime).Seconds()),
		"allowed_users":  s.Allowed,
		"events":         len(s.Events()),
	})
}

func (d *dashboardServer) renderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setSecurityHeaders(w)
	_ = loginTmpl.Execute(w, dashView{
		CSS:   template.CSS(dashboardCSS),
		Error: errMsg,
	})
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// clientIP returns the best-effort client IP, preferring nginx's
// X-Forwarded-For (which is already filtered to trusted CF ranges) before
// falling back to the raw RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
