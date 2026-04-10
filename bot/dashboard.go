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
	Mark       template.HTML // inline SVG of the brand mark, same glyph as the favicon
	Error      string        // for login page only
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
:root {
  color-scheme: dark;
  --bg:        #0a0c10;
  --bg-soft:   #0f131a;
  --card:      #12161f;
  --card-2:    #171c27;
  --border:    #1f2632;
  --border-2:  #2a3444;
  --fg:        #e6e9ef;
  --fg-dim:    #c8d0dd;
  --muted:     #8b94a8;
  --muted-2:   #6b7589;
  --accent:    #6366f1;
  --accent-2:  #8b5cf6;
  --ok:        #34d399;
  --warn:      #f59e0b;
  --err:       #f87171;
  --info:      #60a5fa;
  --radius:    12px;
  --shadow-1:  0 1px 2px rgba(0,0,0,0.4), 0 4px 16px rgba(0,0,0,0.25);
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Inter", ui-sans-serif, system-ui, sans-serif;
  background:
    radial-gradient(1200px 600px at 15% -10%, rgba(99,102,241,0.08), transparent 60%),
    radial-gradient(900px 500px at 90% -20%, rgba(139,92,246,0.06), transparent 60%),
    var(--bg);
  color: var(--fg);
  line-height: 1.5;
  min-height: 100vh;
  -webkit-font-smoothing: antialiased;
}
a { color: var(--info); text-decoration: none; }
a:hover { text-decoration: underline; }
code { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, monospace; font-size: 0.92em; }
.muted { color: var(--muted); }
h1, h2, h3 { letter-spacing: -0.01em; }
h1 { margin: 0; font-size: 22px; font-weight: 700; }
h2 { margin: 28px 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.1em; color: var(--muted); font-weight: 600; }

/* ---------- top nav (authed) ---------- */
.nav {
  position: sticky; top: 0; z-index: 50;
  background: rgba(10,12,16,0.78);
  backdrop-filter: saturate(140%) blur(10px);
  -webkit-backdrop-filter: saturate(140%) blur(10px);
  border-bottom: 1px solid var(--border);
}
.nav-inner { max-width: 1100px; margin: 0 auto; display: flex; align-items: center; gap: 16px; padding: 12px 24px; }
.brand { display: flex; align-items: center; gap: 10px; font-weight: 700; letter-spacing: -0.01em; }
.brand-mark {
  width: 28px; height: 28px;
  display: inline-flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  filter: drop-shadow(0 4px 14px rgba(99,102,241,0.35));
}
.brand-mark svg { width: 100%; height: 100%; display: block; }
.brand-mark.lg { width: 44px; height: 44px; }
.brand-mark.xl { width: 72px; height: 72px; }
.nav a.tab {
  color: var(--fg-dim); font-size: 13px; font-weight: 500;
  padding: 7px 12px; border-radius: 8px; text-decoration: none;
  display: inline-flex; align-items: center; gap: 6px;
}
.nav a.tab:hover { background: var(--card); color: var(--fg); text-decoration: none; }
.nav .spacer { flex: 1; }
.nav .who { font-size: 12px; color: var(--muted); display: flex; align-items: center; gap: 10px; }

/* ---------- container ---------- */
.wrap { max-width: 1100px; margin: 0 auto; padding: 28px 24px 80px; }

/* ---------- hero on authed page ---------- */
.hero-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 22px; flex-wrap: wrap; }
.hero-title { font-size: 24px; font-weight: 700; }
.hero-sub { color: var(--muted); font-size: 13px; margin-top: 2px; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--ok); margin-right: 8px; vertical-align: middle; box-shadow: 0 0 0 3px rgba(52,211,153,0.15); }

/* ---------- cards ---------- */
.card {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px 18px;
  box-shadow: var(--shadow-1);
}
.card h3 { margin: 0 0 8px; font-size: 13px; font-weight: 600; color: var(--fg); }

/* ---------- stats row ---------- */
.stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 10px; }
.stat .k { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.08em; }
.stat .v { font-size: 24px; margin-top: 4px; font-variant-numeric: tabular-nums; font-weight: 600; color: var(--fg); word-break: break-all; }
.stat .hint { font-size: 11px; color: var(--muted-2); margin-top: 2px; }
.stat.accent { background: linear-gradient(180deg, rgba(99,102,241,0.12), rgba(99,102,241,0.02)); border-color: rgba(99,102,241,0.35); }

/* ---------- two-column layout for sections ---------- */
.cols { display: grid; grid-template-columns: 2fr 1fr; gap: 16px; margin-top: 18px; }
@media (max-width: 860px) { .cols { grid-template-columns: 1fr; } }

.section-card { padding: 0; overflow: hidden; }
.section-card > .hd {
  padding: 12px 18px; border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
}
.section-card > .hd .lbl { font-size: 11px; text-transform: uppercase; letter-spacing: 0.1em; color: var(--muted); font-weight: 600; }
.section-card > .hd .count { font-size: 11px; color: var(--muted-2); font-variant-numeric: tabular-nums; }
.section-card > .body { padding: 14px 18px; }
.section-card > .body.tight { padding: 0; }

/* ---------- tables ---------- */
table { width: 100%; border-collapse: collapse; font-size: 13px; }
td, th { padding: 9px 14px; text-align: left; border-bottom: 1px solid var(--border); vertical-align: top; }
tbody tr:last-child td { border-bottom: none; }
th { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.08em; font-weight: 600; background: var(--bg-soft); }
.dir-in { color: var(--info); font-weight: 600; }
.dir-out { color: var(--ok); font-weight: 600; }
.dir-error { color: var(--err); font-weight: 600; }

/* ---------- pre / logs ---------- */
pre {
  background: var(--bg-soft);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 14px 16px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.55;
  color: var(--fg-dim);
  margin: 0;
  max-height: 360px;
}
pre::-webkit-scrollbar { width: 8px; height: 8px; }
pre::-webkit-scrollbar-thumb { background: var(--border-2); border-radius: 8px; }

/* ---------- buttons ---------- */
.btn {
  display: inline-flex; align-items: center; justify-content: center; gap: 6px;
  padding: 8px 14px; border-radius: 8px;
  background: var(--card-2); color: var(--fg); border: 1px solid var(--border-2);
  font-size: 13px; font-weight: 500; cursor: pointer; text-decoration: none;
  transition: background .12s ease, border-color .12s ease, transform .06s ease;
  font-family: inherit;
}
.btn:hover { background: #202636; border-color: #364156; text-decoration: none; }
.btn:active { transform: translateY(1px); }
.btn-primary {
  background: linear-gradient(135deg, var(--accent), var(--accent-2));
  border-color: transparent;
  color: white;
  box-shadow: 0 6px 18px rgba(99,102,241,0.35);
}
.btn-primary:hover { filter: brightness(1.08); background: linear-gradient(135deg, var(--accent), var(--accent-2)); }
.btn-ghost { background: transparent; border-color: var(--border); }
.btn-ghost:hover { background: var(--card); }

/* ---------- footer ---------- */
.foot {
  margin-top: 40px; padding-top: 16px; border-top: 1px solid var(--border);
  font-size: 12px; color: var(--muted); display: flex; justify-content: space-between; flex-wrap: wrap; gap: 10px;
}

/* ---------- login form ---------- */
.login-wrap { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 24px; }
form.login {
  width: 100%; max-width: 380px;
  padding: 32px;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 16px;
  box-shadow: 0 1px 2px rgba(0,0,0,0.4), 0 20px 60px rgba(0,0,0,0.45);
}
form.login .brand { margin-bottom: 6px; }
form.login .sublabel { color: var(--muted); font-size: 13px; margin-bottom: 22px; }
form.login label {
  display: block; font-size: 11px; color: var(--muted);
  text-transform: uppercase; letter-spacing: 0.08em; margin: 12px 0 6px;
  font-weight: 600;
}
form.login input {
  width: 100%;
  padding: 11px 13px;
  background: var(--bg-soft);
  color: var(--fg);
  border: 1px solid var(--border-2);
  border-radius: 9px;
  font-size: 14px;
  font-family: inherit;
  -webkit-appearance: none; appearance: none;
}
form.login input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px rgba(99,102,241,0.2); }
form.login input:-webkit-autofill,
form.login input:-webkit-autofill:hover,
form.login input:-webkit-autofill:focus {
  -webkit-box-shadow: 0 0 0 1000px var(--bg-soft) inset !important;
  -webkit-text-fill-color: var(--fg) !important;
  caret-color: var(--fg);
  transition: background-color 9999s ease-in-out 0s;
}
form.login button { width: 100%; padding: 11px; margin-top: 22px; font-size: 14px; }
.err { color: var(--err); font-size: 13px; margin-top: 14px; padding: 10px 12px; background: rgba(248,113,113,0.1); border: 1px solid rgba(248,113,113,0.25); border-radius: 8px; }

/* ---------- public landing ---------- */
.landing { max-width: 1100px; margin: 0 auto; padding: 0 24px; }
.landing-nav { display: flex; align-items: center; padding: 18px 0; }
.landing-nav .spacer { flex: 1; }
.landing-hero {
  padding: 72px 0 56px;
  text-align: center;
  border-bottom: 1px solid var(--border);
  margin-bottom: 56px;
}
.landing-hero .eyebrow {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border: 1px solid var(--border-2); border-radius: 999px;
  font-size: 11px; text-transform: uppercase; letter-spacing: 0.1em; color: var(--muted);
  background: var(--card); margin-bottom: 18px;
}
.landing-hero h1 {
  font-size: 56px; line-height: 1.05; font-weight: 800; letter-spacing: -0.025em;
  margin: 0 auto; max-width: 760px;
}
.landing-hero h1 span {
  background: linear-gradient(135deg, #c4b5fd, #818cf8);
  -webkit-background-clip: text; background-clip: text; color: transparent;
}
.landing-hero p.tagline {
  margin: 22px auto 0; max-width: 620px; font-size: 17px; color: var(--fg-dim); line-height: 1.55;
}
.landing-hero .cta { display: flex; gap: 12px; justify-content: center; margin-top: 30px; flex-wrap: wrap; }

.section { margin-bottom: 64px; }
.section .eyebrow-lbl {
  font-size: 11px; text-transform: uppercase; letter-spacing: 0.12em;
  color: var(--accent); font-weight: 700; margin-bottom: 8px;
}
.section h2.big { font-size: 28px; margin: 0 0 10px; color: var(--fg); text-transform: none; letter-spacing: -0.01em; }
.section .lede { font-size: 15px; color: var(--muted); max-width: 640px; }

.features-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px; margin-top: 28px;
}
.feature {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 22px;
  transition: border-color .2s ease, transform .2s ease;
}
.feature:hover { border-color: var(--border-2); transform: translateY(-2px); }
.feature .icon {
  width: 36px; height: 36px; border-radius: 10px;
  background: linear-gradient(135deg, rgba(99,102,241,0.25), rgba(139,92,246,0.25));
  border: 1px solid rgba(99,102,241,0.35);
  display: flex; align-items: center; justify-content: center;
  margin-bottom: 14px;
  font-size: 18px;
}
.feature h3 { margin: 0 0 6px; font-size: 15px; font-weight: 600; color: var(--fg); }
.feature p { margin: 0; font-size: 13px; color: var(--muted); line-height: 1.55; }

.steps {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 16px; margin-top: 28px;
  counter-reset: step;
}
.step {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 22px;
  position: relative;
}
.step::before {
  counter-increment: step;
  content: counter(step);
  display: inline-flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; border-radius: 8px;
  background: linear-gradient(135deg, var(--accent), var(--accent-2));
  color: white; font-weight: 700; font-size: 13px;
  margin-bottom: 12px;
}
.step h3 { margin: 0 0 6px; font-size: 15px; font-weight: 600; }
.step p { margin: 0; font-size: 13px; color: var(--muted); line-height: 1.55; }

.stack {
  display: flex; flex-wrap: wrap; gap: 8px; margin-top: 18px;
}
.stack span {
  padding: 6px 12px; border: 1px solid var(--border-2); border-radius: 999px;
  font-size: 12px; color: var(--fg-dim); background: var(--card);
}
`

// The public landing page is intentionally generic — no uptime, no model,
// no bot name, no user counts. Attackers probing the domain should learn
// nothing about what's running here or how many users it has.
const publicHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw — self-hosted coding agent control plane</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#0a0c10">
<style>{{.CSS}}</style>
</head><body>

<div class=landing>
  <div class=landing-nav>
    <div class=brand>
      <div class=brand-mark>{{.Mark}}</div>
      <div>openclaw</div>
    </div>
    <div class=spacer></div>
    <a class="btn btn-ghost" href="https://github.com/anchoo2kewl/openclaw">GitHub</a>
    <a class="btn btn-primary" href="/login" style="margin-left:8px">Sign in</a>
  </div>

  <section class=landing-hero>
    <div class=eyebrow>● Self-hosted · open source · MIT</div>
    <h1>Your private <span>coding agent</span>,<br>reachable from anywhere.</h1>
    <p class=tagline>
      Drive a sandboxed agent from a chat interface on any device.
      Private by default, hosted on your own box, owned by you.
    </p>
    <div class=cta>
      <a class="btn btn-primary" href="/login">Sign in to your console</a>
      <a class="btn btn-ghost" href="https://github.com/anchoo2kewl/openclaw">View on GitHub →</a>
    </div>
  </section>

  <section class=section>
    <div class=eyebrow-lbl>FEATURES</div>
    <h2 class=big>Everything you need, nothing you don't.</h2>
    <p class=lede>A minimal operator console over a focused set of primitives: chat, sessions, a workspace, and a handful of integrations.</p>

    <div class=features-grid>
      <div class=feature>
        <div class=icon>💬</div>
        <h3>Chat-driven workflows</h3>
        <p>Send a message, get a result. Long-running agent loops stream progress back to you as they finish.</p>
      </div>
      <div class=feature>
        <div class=icon>🛡</div>
        <h3>Private allowlist</h3>
        <p>No signups, no public access. Only explicitly approved accounts can interact with the agent.</p>
      </div>
      <div class=feature>
        <div class=icon>⏱</div>
        <h3>Resumable sessions</h3>
        <p>Each account gets its own persistent conversation and a dedicated workspace on disk.</p>
      </div>
      <div class=feature>
        <div class=icon>⚙</div>
        <h3>Container sandbox</h3>
        <p>Commands run inside a disposable container with a scoped workspace volume and no host access.</p>
      </div>
      <div class=feature>
        <div class=icon>📈</div>
        <h3>Operator console</h3>
        <p>Live view of sessions, recent activity, workspace files, and server logs — all in one place.</p>
      </div>
      <div class=feature>
        <div class=icon>🔌</div>
        <h3>Integrated gateway</h3>
        <p>Embedded upstream coding-agent gateway for a full browser chat UI, proxied through your own auth.</p>
      </div>
      <div class=feature>
        <div class=icon>📦</div>
        <h3>Single static binary</h3>
        <p>Pure Go standard library. No framework sprawl, no runtime dependencies — easy to audit and deploy.</p>
      </div>
      <div class=feature>
        <div class=icon>🔐</div>
        <h3>Cookie session auth</h3>
        <p>Password-protected management console with PBKDF2-hashed credentials. No third-party IdP required.</p>
      </div>
      <div class=feature>
        <div class=icon>🧭</div>
        <h3>Ansible-reproducible</h3>
        <p>Every piece of the stack is templated. Blow away the VM, re-run the playbook, get the same box back.</p>
      </div>
    </div>
  </section>

  <section class=section>
    <div class=eyebrow-lbl>HOW IT WORKS</div>
    <h2 class=big>From zero to agent in three steps.</h2>

    <div class=steps>
      <div class=step>
        <h3>Provision</h3>
        <p>Run the ansible playbook against any Ubuntu host. You get nginx, Docker, a hardened bot container, and a sibling gateway container — wired together.</p>
      </div>
      <div class=step>
        <h3>Add accounts</h3>
        <p>Provision operators with <code>openclaw useradd</code>. PBKDF2-hashed passwords live in a single JSON file, bind-mounted into the bot.</p>
      </div>
      <div class=step>
        <h3>Sign in and chat</h3>
        <p>Log into the console from any browser. Open the embedded gateway for a rich chat UI, or drive the agent from Telegram.</p>
      </div>
    </div>
  </section>

  <section class=section>
    <div class=eyebrow-lbl>UNDER THE HOOD</div>
    <h2 class=big>Boring, auditable, reproducible.</h2>
    <p class=lede>A small Go binary and a couple of containers. No background magic.</p>

    <div class=stack>
      <span>Go 1.26 · stdlib only</span>
      <span>zerolog</span>
      <span>PBKDF2-SHA256</span>
      <span>nginx reverse proxy</span>
      <span>Cloudflare origin</span>
      <span>Docker Compose</span>
      <span>Ansible</span>
      <span>Claude Code CLI</span>
      <span>Telegram Bot API</span>
    </div>
  </section>

  <div class=foot>
    <div>self-hosted · open source · MIT</div>
    <div><a href="https://github.com/anchoo2kewl/openclaw">github.com/anchoo2kewl/openclaw</a></div>
  </div>
</div>

</body></html>`

// The authed dashboard is the operational view — everything sensitive lives
// here and nowhere else.
const dashboardHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<meta http-equiv=refresh content=15>
<title>openclaw · console</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#0a0c10">
<style>{{.CSS}}</style>
</head><body>

<nav class=nav>
  <div class=nav-inner>
    <div class=brand>
      <div class=brand-mark>{{.Mark}}</div>
      <div>openclaw</div>
    </div>
    <a class=tab href="/">Overview</a>
    {{if .HasGateway}}<a class=tab href="/gateway-launch">Gateway ↗</a>{{end}}
    <a class=tab href="#activity">Activity</a>
    <a class=tab href="#accounts">Accounts</a>
    <a class=tab href="#logs">Logs</a>
    <div class=spacer></div>
    <div class=who>
      <span>● {{.Email}}</span>
      <form method=POST action="/logout" style="margin:0">
        <button class="btn btn-ghost" type=submit>Log out</button>
      </form>
    </div>
  </div>
</nav>

<main class=wrap>

  <div class=hero-row>
    <div>
      <div class=hero-title>Operator console</div>
      <div class=hero-sub><span class=dot></span>online · uptime {{.Uptime}} · refreshes every 15s</div>
    </div>
    <div style="display:flex;gap:8px">
      {{if .HasGateway}}<a class="btn btn-primary" href="/gateway-launch">Open gateway →</a>{{end}}
    </div>
  </div>

  <!-- ---- stat cards ---- -->
  <div class=stats>
    <div class="card stat accent">
      <div class=k>Active sessions</div>
      <div class=v>{{len .Sessions}}</div>
      <div class=hint>Telegram conversations currently held</div>
    </div>
    <div class="card stat">
      <div class=k>Messages seen</div>
      <div class=v>{{len .Events}}</div>
      <div class=hint>Ring-buffered (last 200)</div>
    </div>
    <div class="card stat">
      <div class=k>Operators</div>
      <div class=v>{{len .Users}}</div>
      <div class=hint>Dashboard accounts provisioned</div>
    </div>
    <div class="card stat">
      <div class=k>Telegram allowlist</div>
      <div class=v>{{len .Allowed}}</div>
      <div class=hint>User ids allowed to DM the bot</div>
    </div>
  </div>

  <!-- ---- configuration strip ---- -->
  <div class=stats style="margin-top:12px">
    <div class=card>
      <div class=k style="font-size:10px;text-transform:uppercase;letter-spacing:0.08em;color:var(--muted)">Model</div>
      <div style="font-size:14px;font-variant-numeric:tabular-nums;margin-top:4px">{{.Model}}</div>
    </div>
    <div class=card>
      <div class=k style="font-size:10px;text-transform:uppercase;letter-spacing:0.08em;color:var(--muted)">Workspace</div>
      <div style="font-size:14px;font-variant-numeric:tabular-nums;margin-top:4px;word-break:break-all"><code>{{.Workspace}}</code></div>
    </div>
    <div class=card>
      <div class=k style="font-size:10px;text-transform:uppercase;letter-spacing:0.08em;color:var(--muted)">Allowed Telegram IDs</div>
      <div style="font-size:14px;margin-top:4px;word-break:break-all">
        {{if .Allowed}}{{range $i, $u := .Allowed}}{{if $i}}, {{end}}<code>{{$u}}</code>{{end}}{{else}}<span class=muted>(none)</span>{{end}}
      </div>
    </div>
  </div>

  <!-- ---- two-column: activity + sidebar ---- -->
  <div class=cols>
    <div>

      <div id=activity class="card section-card" style="margin-top:4px">
        <div class=hd>
          <div class=lbl>Recent activity</div>
          <div class=count>{{len .Events}} events</div>
        </div>
        <div class="body tight">
          {{if .Events}}
          <table>
            <thead><tr><th style="width:80px">Time</th><th style="width:60px">Dir</th><th style="width:110px">User</th><th>Message</th></tr></thead>
            <tbody>
            {{range .Events}}<tr><td class=muted>{{fmtTime .Time}}</td><td class="dir-{{.Direction}}">{{.Direction}}</td><td><code>{{.UserID}}</code></td><td>{{.Text}}</td></tr>{{end}}
            </tbody>
          </table>
          {{else}}
          <div style="padding:18px;text-align:center" class=muted>No messages yet — ping <code>@clawdy</code> on Telegram to see events flow here.</div>
          {{end}}
        </div>
      </div>

      <div class="card section-card" style="margin-top:16px">
        <div class=hd>
          <div class=lbl>Telegram sessions</div>
          <div class=count>{{len .Sessions}} active</div>
        </div>
        <div class="body tight">
          {{if .Sessions}}
          <table>
            <thead><tr><th>User</th><th>Session id</th><th>Workspace</th></tr></thead>
            <tbody>
            {{range .Sessions}}<tr><td><code>{{.UserID}}</code></td><td><code>{{if .SessionID}}{{.SessionID}}{{else}}—{{end}}</code></td><td><code>{{.Cwd}}</code></td></tr>{{end}}
            </tbody>
          </table>
          {{else}}
          <div style="padding:18px;text-align:center" class=muted>No active sessions. A session is created when an allowed user sends their first message.</div>
          {{end}}
        </div>
      </div>

      <div id=logs class="card section-card" style="margin-top:16px">
        <div class=hd>
          <div class=lbl>Server logs (tail)</div>
          <div class=count>{{len .Logs}} lines</div>
        </div>
        <div class=body>
          {{if .Logs}}<pre>{{range .Logs}}{{.}}
{{end}}</pre>{{else}}<div class=muted>No logs yet.</div>{{end}}
        </div>
      </div>

    </div>

    <div>
      <div id=accounts class="card section-card">
        <div class=hd>
          <div class=lbl>Dashboard accounts</div>
          <div class=count>{{len .Users}} total</div>
        </div>
        <div class="body tight">
          {{if .Users}}
          <table>
            <thead><tr><th>Username</th><th>Email</th></tr></thead>
            <tbody>
            {{range .Users}}<tr><td><code>{{.Username}}</code></td><td>{{.Email}}</td></tr>{{end}}
            </tbody>
          </table>
          {{else}}<div style="padding:14px" class=muted>No accounts provisioned.</div>{{end}}
        </div>
      </div>

      <div class="card section-card" style="margin-top:16px">
        <div class=hd>
          <div class=lbl>Workspace files</div>
          <div class=count>{{len .Files}} items</div>
        </div>
        <div class="body tight">
          {{if .Files}}
          <table>
            <thead><tr><th>Path</th><th style="width:72px;text-align:right">Size</th></tr></thead>
            <tbody>
            {{range .Files}}<tr><td><code>{{.Path}}</code></td><td style="text-align:right" class=muted>{{fmtSize .Size}}</td></tr>{{end}}
            </tbody>
          </table>
          {{else}}<div style="padding:14px" class=muted>Workspace is empty.</div>{{end}}
        </div>
      </div>

      <div class="card section-card" style="margin-top:16px">
        <div class=hd><div class=lbl>Helpful links</div></div>
        <div class=body style="font-size:13px;line-height:1.9">
          {{if .HasGateway}}<div><a href="/gateway-launch">Open gateway console →</a></div>{{end}}
          <div><a href="/api/status">/api/status (JSON)</a></div>
          <div><a href="/health">/health</a></div>
          <div><a href="https://github.com/anchoo2kewl/openclaw">Source on GitHub →</a></div>
        </div>
      </div>
    </div>
  </div>

  <div class=foot>
    <div>refreshes every 15s · {{.Bot}}</div>
    <div><a href="https://github.com/anchoo2kewl/openclaw">github.com/anchoo2kewl/openclaw</a></div>
  </div>
</main>
</body></html>`

const loginHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw · sign in</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#0a0c10">
<style>{{.CSS}}</style>
</head><body>
<div class=login-wrap>
  <form class=login method=POST action="/login">
    <div class=brand style="gap:12px;margin-bottom:8px">
      <div class="brand-mark lg">{{.Mark}}</div>
      <div style="font-size:22px">openclaw</div>
    </div>
    <div class=sublabel>Sign in to your operator console</div>
    <label for=identifier>Email or username</label>
    <input id=identifier name=identifier type=text autocomplete=username autofocus required placeholder="admin">
    <label for=password>Password</label>
    <input id=password name=password type=password autocomplete=current-password required placeholder="••••••••">
    <button class="btn btn-primary" type=submit>Sign in →</button>
    {{if .Error}}<div class=err>Invalid credentials</div>{{end}}
    <div class=foot style="margin-top:26px;padding-top:16px">
      <a href="/">← Back to home</a>
      <a href="https://github.com/anchoo2kewl/openclaw">GitHub</a>
    </div>
  </form>
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
	mux.HandleFunc("/favicon.svg", serveFaviconSVG)
	mux.HandleFunc("/favicon.ico", serveFaviconSVG) // browsers fall back here; we serve SVG with the right content-type
	mux.HandleFunc("/apple-touch-icon.png", serveFaviconSVG)
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
	_ = publicTmpl.Execute(w, dashView{
		CSS:  template.CSS(dashboardCSS),
		Mark: brandMarkHTML,
	})
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
		Mark:       brandMarkHTML,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashTmpl.Execute(w, view)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// faviconSVG is the brand mark — a rounded square with a hex/claw outline
// and a centred dot, rendered with an indigo→violet gradient. Same exact
// SVG used for /favicon.svg, the on-page brand-mark element across every
// template, AND the back-bar overlay injected into the gateway HTML, so
// the brand stays consistent on every surface.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#6366f1"/>
      <stop offset="1" stop-color="#8b5cf6"/>
    </linearGradient>
  </defs>
  <rect x="2" y="2" width="60" height="60" rx="14" fill="url(#g)"/>
  <path d="M20 22 L32 14 L44 22 L44 42 L32 50 L20 42 Z" fill="none" stroke="#fff" stroke-width="3.5" stroke-linejoin="round"/>
  <circle cx="32" cy="32" r="4.5" fill="#fff"/>
</svg>`

// brandMarkHTML is the same SVG, marked safe for use inside templates.
var brandMarkHTML = template.HTML(faviconSVG)

func serveFaviconSVG(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
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
		Mark:  brandMarkHTML,
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
