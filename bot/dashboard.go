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
)

// dashView is the template model passed to the HTML renderer.
type dashView struct {
	Bot       string
	Model     string
	Allowed   []int64
	Workspace string
	Uptime    string
	Sessions  []Session
	Events    []Event
	Files     []fileEntry
	Logs      []string
}

type fileEntry struct {
	Path string
	Size int64
}

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

const dashboardCSS = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", ui-sans-serif, system-ui, sans-serif; background: #0b0d10; color: #e6e9ef; max-width: 960px; margin: 0 auto; line-height: 1.45; }
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
`

const dashboardHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<meta http-equiv=refresh content=10>
<title>openclaw · {{.Bot}}</title>
<style>{{.CSS}}</style>
</head><body>
<h1>openclaw <span class=muted style="font-size:14px">/ {{.Bot}}</span></h1>
<div class=sub><span class=dot></span>online · uptime {{.Uptime}} · {{len .Sessions}} active session(s)</div>

<div class=grid>
  <div class=card><div class=k>model</div><div class=v>{{.Model}}</div></div>
  <div class=card><div class=k>allowed users</div><div class=v>{{if .Allowed}}{{range $i, $u := .Allowed}}{{if $i}}, {{end}}{{$u}}{{end}}{{else}}(none){{end}}</div></div>
  <div class=card><div class=k>workspace</div><div class=v>{{.Workspace}}</div></div>
</div>

<h2>sessions</h2>
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

var dashTmpl = template.Must(template.New("dash").Funcs(template.FuncMap{
	"fmtTime": func(t time.Time) string { return t.Format("15:04:05") },
	"fmtSize": fmtSize,
}).Parse(dashboardHTML))

// NewDashboard returns an http.Handler serving the dashboard + a small JSON
// status endpoint + an unauthenticated /health probe.
func NewDashboard(s *State) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bot":            s.BotName,
			"model":          s.Model,
			"uptime_seconds": int(time.Since(s.StartTime).Seconds()),
			"allowed_users":  s.Allowed,
			"events":         len(s.Events()),
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		sess := s.SessionsSnapshot()
		sort.Slice(sess, func(i, j int) bool { return sess[i].UserID < sess[j].UserID })

		view := struct {
			dashView
			CSS template.CSS
		}{
			dashView: dashView{
				Bot:       s.BotName,
				Model:     orDefault(s.Model, "(default)"),
				Allowed:   s.Allowed,
				Workspace: s.Workspace,
				Uptime:    fmtUptime(time.Since(s.StartTime)),
				Sessions:  sess,
				Events:    s.Events(),
				Files:     listWorkspace(s.Workspace),
				Logs:      s.Logs(),
			},
			CSS: template.CSS(dashboardCSS),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dashTmpl.Execute(w, view)
	})

	return mux
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
