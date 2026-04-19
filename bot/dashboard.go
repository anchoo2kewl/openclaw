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
@import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500;600;700;800&family=Inter+Tight:wght@400;500;600;700;800;900&display=swap');
:root {
  --bg: #05070a;
  --bg-1: #0a0d12;
  --bg-2: #0f1319;
  --bg-3: #151a22;
  --panel: #0b0f14;
  --panel-hi: #111722;
  --line: rgba(120, 255, 170, 0.08);
  --line-hi: rgba(120, 255, 170, 0.16);
  --line-strong: rgba(120, 255, 170, 0.28);
  --ink: #dbe6d9;
  --ink-dim: #8a9a8e;
  --ink-faint: #5a6a62;
  --ink-ghost: #36433c;
  --phosphor: oklch(0.85 0.18 145);
  --phosphor-dim: oklch(0.62 0.14 145);
  --phosphor-wash: oklch(0.85 0.18 145 / 0.12);
  --cyan: oklch(0.82 0.12 215);
  --amber: oklch(0.82 0.15 75);
  --red: oklch(0.7  0.21 20);
  --violet: oklch(0.72 0.16 295);
  --r-s: 3px;
  --r-m: 6px;
  --r-l: 10px;
  --glow: 0 0 0 1px rgba(120,255,170,0.12), 0 0 40px -12px rgba(120,255,170,0.25);
  --glow-hi: 0 0 0 1px rgba(120,255,170,0.35), 0 0 60px -10px rgba(120,255,170,0.5);
}
* { box-sizing: border-box; }
html, body {
  margin: 0; padding: 0;
  background: var(--bg);
  color: var(--ink);
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-feature-settings: 'ss01', 'ss02', 'cv11';
  font-size: 14px;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
  text-rendering: geometricPrecision;
}
body {
  min-height: 100vh;
  background-image:
    radial-gradient(ellipse 80% 60% at 50% -10%, rgba(120,255,170,0.06), transparent 60%),
    radial-gradient(ellipse 60% 40% at 100% 0%, rgba(120,200,255,0.03), transparent 60%),
    linear-gradient(to bottom, #05070a 0%, #04060a 100%);
  background-attachment: fixed;
}
body::before {
  content: '';
  position: fixed; inset: 0;
  pointer-events: none;
  background: repeating-linear-gradient(to bottom, transparent 0, transparent 2px, rgba(0,0,0,0.15) 2px, rgba(0,0,0,0.15) 3px);
  mix-blend-mode: multiply;
  opacity: 0.5;
  z-index: 9999;
}
body::after {
  content: '';
  position: fixed; inset: 0;
  pointer-events: none;
  background-image:
    linear-gradient(to right, rgba(120,255,170,0.025) 1px, transparent 1px),
    linear-gradient(to bottom, rgba(120,255,170,0.025) 1px, transparent 1px);
  background-size: 64px 64px;
  z-index: 1;
  mask-image: radial-gradient(ellipse 120% 80% at 50% 30%, black, transparent 80%);
}
.prose { font-family: 'Inter Tight', ui-sans-serif, system-ui, sans-serif; }
.mono { font-family: 'JetBrains Mono', ui-monospace, monospace; }
.kicker {
  font-family: 'JetBrains Mono', monospace;
  font-size: 11px; letter-spacing: 0.16em;
  text-transform: uppercase; color: var(--phosphor);
}
.eyebrow {
  font-family: 'JetBrains Mono', monospace;
  font-size: 10px; letter-spacing: 0.2em;
  text-transform: uppercase; color: var(--ink-faint);
}
.ink-dim { color: var(--ink-dim); }
.ink-faint { color: var(--ink-faint); }
.numeric { font-variant-numeric: tabular-nums; font-feature-settings: 'tnum'; }
.cursor {
  display: inline-block; width: 0.55em; height: 1em;
  background: var(--phosphor); vertical-align: -0.15em;
  animation: blink 1.05s steps(2, end) infinite;
  margin-left: 0.15em;
  box-shadow: 0 0 6px var(--phosphor);
}
@keyframes blink { 50% { opacity: 0; } }

/* Buttons */
.btn {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 16px; background: transparent;
  border: 1px solid var(--line-hi); color: var(--ink);
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px; font-weight: 500;
  letter-spacing: 0.06em; text-transform: uppercase;
  cursor: pointer; border-radius: var(--r-s);
  transition: all 0.12s ease; text-decoration: none;
}
.btn:hover { border-color: var(--phosphor); color: var(--phosphor); background: var(--phosphor-wash); }
.btn-primary { background: var(--phosphor); color: #041008; border-color: var(--phosphor); font-weight: 700; }
.btn-primary:hover { background: transparent; color: var(--phosphor); box-shadow: var(--glow-hi); }
.btn-ghost { border-color: transparent; color: var(--ink-dim); }
.btn-ghost:hover { color: var(--phosphor); background: transparent; }

/* Panel / Card */
.panel { background: var(--panel); border: 1px solid var(--line); border-radius: var(--r-m); position: relative; }
.panel-hi { background: var(--panel-hi); border: 1px solid var(--line-hi); border-radius: var(--r-m); }

/* Corner tics */
.corner { position: relative; }
.corner::before, .corner::after {
  content: ''; position: absolute; width: 8px; height: 8px;
  border-color: var(--phosphor); border-style: solid; opacity: 0.5;
}
.corner::before { top: -1px; left: -1px; border-width: 1px 0 0 1px; }
.corner::after { bottom: -1px; right: -1px; border-width: 0 1px 1px 0; }

/* Status dot */
.dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; vertical-align: 0.1em; }
.dot.ok { background: var(--phosphor); box-shadow: 0 0 8px var(--phosphor); }
.dot.warn { background: var(--amber); box-shadow: 0 0 8px var(--amber); }
.dot.err { background: var(--red); box-shadow: 0 0 8px var(--red); }
.dot.idle { background: var(--ink-ghost); }

/* Pulse */
@keyframes pulse-ring {
  0% { box-shadow: 0 0 0 0 rgba(120,255,170,0.5); }
  100% { box-shadow: 0 0 0 10px rgba(120,255,170,0); }
}
.pulse { animation: pulse-ring 1.6s ease-out infinite; border-radius: 50%; }

/* Chip */
.chip {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 3px 8px; font-size: 10.5px;
  letter-spacing: 0.08em; text-transform: uppercase;
  border: 1px solid var(--line-hi); border-radius: 3px;
  color: var(--ink-dim); background: rgba(10,14,18,0.6);
}
.chip-green { color: var(--phosphor); border-color: rgba(120,255,170,0.3); }
.chip-amber { color: var(--amber); border-color: rgba(255,180,70,0.3); }
.chip-cyan { color: var(--cyan); border-color: rgba(130,210,255,0.3); }

/* Inputs */
input, textarea, select {
  background: var(--bg-1); border: 1px solid var(--line);
  color: var(--ink); font-family: inherit; font-size: 13px;
  padding: 8px 12px; border-radius: var(--r-s); outline: none;
}
input:focus, textarea:focus, select:focus {
  border-color: var(--phosphor);
  box-shadow: 0 0 0 3px var(--phosphor-wash);
}
input:-webkit-autofill,
input:-webkit-autofill:hover,
input:-webkit-autofill:focus {
  -webkit-box-shadow: 0 0 0 1000px var(--bg-1) inset !important;
  -webkit-text-fill-color: var(--ink) !important;
  caret-color: var(--ink);
  transition: background-color 9999s ease-in-out 0s;
}

/* Scrollbars */
::-webkit-scrollbar { width: 10px; height: 10px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--line-hi); border-radius: 4px; border: 2px solid transparent; background-clip: padding-box; }
::-webkit-scrollbar-thumb:hover { background: var(--line-strong); background-clip: padding-box; border: 2px solid transparent; }

/* Links */
a { color: var(--phosphor); text-decoration: none; transition: opacity 0.1s; }
a:hover { opacity: 0.8; }

/* Selection */
::selection { background: var(--phosphor); color: #000; }

/* Utility */
.container { max-width: 1280px; margin: 0 auto; padding: 0 32px; }
.divider { border: none; height: 1px; background: var(--line); margin: 0; }
.divider-dashed { border: none; border-top: 1px dashed var(--line-hi); }

/* Brand logo */
.claw-logo {
  display: inline-flex; align-items: center; gap: 10px;
  font-family: 'JetBrains Mono', monospace;
  font-weight: 700; font-size: 13px;
  letter-spacing: 0.1em; color: var(--ink);
  text-transform: uppercase; text-decoration: none;
}
.claw-logo .mark {
  width: 22px; height: 22px;
  display: grid; place-items: center;
  background: var(--phosphor); color: #041008;
  border-radius: 3px; font-weight: 900; font-size: 13px;
  box-shadow: 0 0 12px -2px var(--phosphor);
}

/* Global nav */
.gnav {
  position: sticky; top: 0; z-index: 50;
  backdrop-filter: blur(16px);
  background: rgba(5,7,10,0.72);
  border-bottom: 1px solid var(--line);
}
.gnav-inner {
  display: flex; align-items: center; gap: 28px; height: 56px;
}
.gnav-links {
  display: flex; gap: 22px;
  font-size: 12px; font-family: 'JetBrains Mono', monospace;
  letter-spacing: 0.06em; text-transform: uppercase;
}
.gnav-links a { color: var(--ink-dim); position: relative; padding: 4px 0; }
.gnav-links a.active, .gnav-links a:hover { color: var(--phosphor); }
.gnav-links a.active::after {
  content: ''; position: absolute;
  left: 0; right: 0; bottom: -19px; height: 1px;
  background: var(--phosphor); box-shadow: 0 0 8px var(--phosphor);
}
.gnav-right {
  margin-left: auto; display: flex; align-items: center;
  gap: 16px; font-size: 12px; color: var(--ink-faint);
}

/* Code block */
.code {
  font-family: 'JetBrains Mono', monospace;
  background: var(--bg-1); border: 1px solid var(--line);
  border-radius: var(--r-s); padding: 16px 18px;
  font-size: 12.5px; line-height: 1.7; color: var(--ink);
  white-space: pre; overflow-x: auto;
}
.code .kw { color: var(--cyan); }
.code .str { color: var(--phosphor); }
.code .cm { color: var(--ink-faint); font-style: italic; }
.code .nm { color: var(--amber); }

/* Flash-in animation */
@keyframes flash-in {
  0% { opacity: 0; transform: translateY(4px); }
  100% { opacity: 1; transform: none; }
}
.flash-in { animation: flash-in 0.24s ease-out both; }

/* Tables */
table { width: 100%; border-collapse: collapse; font-size: 13px; }
td, th { padding: 11px 14px; text-align: left; border-bottom: 1px solid var(--line); }
tbody tr:last-child td { border-bottom: none; }
th {
  font-size: 10px; color: var(--ink-faint); text-transform: uppercase;
  letter-spacing: 0.1em; font-weight: 500; background: var(--bg-1);
  border-bottom: 1px solid var(--line);
}
tbody tr:hover { background: rgba(120,255,170,0.02); }

/* Error */
.err-msg {
  color: var(--red); font-size: 13px; margin-top: 14px;
  padding: 10px 12px; background: rgba(220,60,60,0.1);
  border: 1px solid rgba(220,60,60,0.25); border-radius: var(--r-s);
}

/* Pre / logs */
pre {
  background: #070a0d;
  border: 1px solid var(--line);
  border-radius: var(--r-s);
  padding: 14px 18px; overflow: auto;
  font-family: 'JetBrains Mono', monospace;
  font-size: 11.5px; line-height: 1.7;
  color: #8fb096; margin: 0; max-height: 360px;
  white-space: pre-wrap;
}

/* Footer */
.foot {
  border-top: 1px solid var(--line); padding: 64px 0 32px;
  position: relative; z-index: 2;
}
.foot-top { display: grid; grid-template-columns: 1fr 1.4fr; gap: 60px; }
.foot-cols { display: grid; grid-template-columns: repeat(4, 1fr); gap: 32px; }
.foot-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 10px; }
.foot-list a { color: var(--ink-dim); font-size: 13px; }
.foot-list a:hover { color: var(--phosphor); }
.foot-bot {
  margin-top: 48px; padding-top: 20px;
  border-top: 1px solid var(--line);
  display: flex; justify-content: space-between;
  font-size: 11px; color: var(--ink-faint);
}
@media (max-width: 800px) { .foot-top { grid-template-columns: 1fr; } .foot-cols { grid-template-columns: repeat(2, 1fr); } }

/* ======== LANDING SPECIFIC ======== */
.hero { padding: 56px 0 96px; position: relative; z-index: 2; }
.hero-inner { display: grid; grid-template-columns: 1fr 440px; gap: 80px; align-items: start; }
.hero-title {
  font-family: 'Inter Tight', sans-serif;
  font-weight: 800; font-size: clamp(56px, 7vw, 88px);
  line-height: 0.94; letter-spacing: -0.035em;
  margin: 0 0 28px; color: #f5faf4;
}
.hero-hl {
  background: linear-gradient(180deg, var(--phosphor) 0%, oklch(0.7 0.18 145) 100%);
  -webkit-background-clip: text; background-clip: text;
  color: transparent; text-shadow: 0 0 40px rgba(120,255,170,0.15);
}
.hero-sub { font-size: 18px; line-height: 1.55; color: var(--ink-dim); max-width: 540px; margin: 0 0 36px; }
.hero-cta { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 64px; }
.hero-stats {
  display: grid; grid-template-columns: repeat(4, 1fr);
  border: 1px solid var(--line);
  background: rgba(10,13,18,0.6); backdrop-filter: blur(4px);
}
.hs-cell { padding: 18px 20px; border-right: 1px dashed var(--line); }
.hs-cell:last-child { border-right: none; }
.hs-num {
  font-family: 'JetBrains Mono', monospace;
  font-size: 28px; font-weight: 600; color: #f5faf4;
  margin: 6px 0 4px; letter-spacing: -0.02em;
}
.hs-delta { font-size: 11px; color: var(--ink-faint); }
.hero-right { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; }
.hero-right-label { display: flex; justify-content: space-between; align-items: center; width: 380px; }
.hero-right-caption { width: 380px; font-size: 12.5px; line-height: 1.55; }
@media (max-width: 1024px) {
  .hero-inner { grid-template-columns: 1fr; }
  .hero-stats { grid-template-columns: repeat(2, 1fr); }
  .hs-cell:nth-child(2) { border-right: none; }
  .hs-cell:nth-child(1), .hs-cell:nth-child(2) { border-bottom: 1px dashed var(--line); }
  .hero-right-label, .hero-right-caption { width: 100%; max-width: 380px; }
}

/* Architecture */
.arch { padding: 96px 0; position: relative; z-index: 2; }
.section-head { display: grid; grid-template-columns: 1fr 1fr; gap: 60px; margin-bottom: 48px; align-items: end; }
.section-title {
  font-family: 'Inter Tight', sans-serif;
  font-size: clamp(36px, 4vw, 52px); font-weight: 700;
  line-height: 1.02; letter-spacing: -0.03em;
  color: #f5faf4; margin: 8px 0 0;
}
.section-lede { font-size: 15px; line-height: 1.6; margin: 0; max-width: 520px; }
.arch-diagram { padding: 48px 40px; }
.arch-row { display: flex; align-items: center; justify-content: space-between; }
.arch-node {
  flex: 0 0 auto; width: 150px; padding: 20px 16px;
  border: 1px solid var(--line); background: var(--bg-1);
  border-radius: 4px; text-align: center; transition: all 0.3s;
}
.arch-node.on {
  border-color: var(--phosphor);
  background: rgba(120,255,170,0.06);
  box-shadow: 0 0 30px -8px rgba(120,255,170,0.4);
}
.arch-node-icon {
  width: 40px; height: 40px;
  border: 1px solid var(--line-hi); border-radius: 50%;
  display: grid; place-items: center;
  margin: 0 auto 12px; color: var(--ink-dim); transition: all 0.3s;
}
.arch-node.on .arch-node-icon { border-color: var(--phosphor); color: var(--phosphor); box-shadow: 0 0 20px -4px var(--phosphor); }
.arch-node-label { font-weight: 600; font-size: 13px; color: #e9efe8; }
.arch-node-sub { font-size: 10.5px; color: var(--ink-faint); margin-top: 4px; letter-spacing: 0.04em; }
.arch-edge { flex: 1; position: relative; height: 2px; margin: 0 8px; }
.arch-edge-line { position: absolute; top: 0; left: 0; right: 0; height: 1px; background: var(--line-hi); overflow: hidden; }
.arch-edge-line::before {
  content: ''; position: absolute; left: -30%; top: -1px;
  width: 30%; height: 3px;
  background: linear-gradient(to right, transparent, var(--phosphor), transparent);
  opacity: 0; transition: opacity 0.2s;
}
.arch-edge-line.on::before { opacity: 1; animation: arch-flow 2s linear infinite; }
@keyframes arch-flow { 0% { left: -30%; } 100% { left: 100%; } }
.arch-edge-label {
  position: absolute; top: -18px; left: 50%; transform: translateX(-50%);
  font-size: 10px; color: var(--ink-faint); letter-spacing: 0.1em; text-transform: uppercase;
}
.arch-details {
  margin-top: 40px; display: grid; grid-template-columns: repeat(4, 1fr);
  border-top: 1px dashed var(--line); padding-top: 24px; gap: 24px;
}
@media (max-width: 900px) {
  .section-head { grid-template-columns: 1fr; }
  .arch-row { flex-direction: column; gap: 16px; }
  .arch-edge { width: 100%; }
  .arch-details { grid-template-columns: repeat(2, 1fr); }
}

/* Security */
.sec { padding: 96px 0; position: relative; z-index: 2; }
.sec-grid {
  display: grid; grid-template-columns: repeat(3, 1fr);
  gap: 1px; background: var(--line); border: 1px solid var(--line);
}
.sec-card { padding: 28px 24px; background: var(--bg); border: none !important; border-radius: 0 !important; }
.sec-card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.sec-card-icon {
  width: 36px; height: 36px;
  border: 1px solid var(--line-hi); display: grid; place-items: center;
  color: var(--phosphor); border-radius: 3px;
}
.sec-card-title { font-size: 17px; font-weight: 600; color: #f5faf4; margin-bottom: 8px; }
.sec-card-desc { font-size: 13.5px; line-height: 1.55; }
@media (max-width: 900px) { .sec-grid { grid-template-columns: 1fr; } }

/* Install */
.install { padding: 48px 0; position: relative; z-index: 2; }
.install-wrap { display: grid; grid-template-columns: 1fr 1.1fr; gap: 60px; padding: 56px; }
.terminal { background: #070a0d; border: 1px solid var(--line-hi); border-radius: 6px; overflow: hidden; box-shadow: 0 20px 60px -20px rgba(0,0,0,0.6); }
.terminal-top { display: flex; align-items: center; gap: 6px; padding: 10px 14px; background: #0d1217; border-bottom: 1px solid var(--line); }
.tt-dot { width: 11px; height: 11px; border-radius: 50%; display: inline-block; }
.tt-dot.r { background: #ff5f56; } .tt-dot.y { background: #ffbd2e; } .tt-dot.g { background: #27c93f; }
.tt-copy {
  margin-left: auto; background: transparent;
  border: 1px solid var(--line-hi); color: var(--ink-dim);
  font-family: inherit; font-size: 10.5px;
  letter-spacing: 0.08em; text-transform: uppercase;
  padding: 4px 9px; display: inline-flex; align-items: center; gap: 4px;
  cursor: pointer; border-radius: 2px;
}
.tt-copy:hover { color: var(--phosphor); border-color: var(--phosphor); }
.terminal-body {
  margin: 0; padding: 20px 22px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12.5px; line-height: 1.7;
  color: #cfe2cf; white-space: pre-wrap;
}
.terminal-body .cm { color: #5a6a62; }
.terminal-body .tp { color: var(--phosphor); font-weight: 600; }
.terminal-body .ok { color: var(--phosphor); }
.terminal-body .str { color: var(--cyan); }
@media (max-width: 900px) { .install-wrap { grid-template-columns: 1fr; padding: 32px; } }

/* Roadmap */
.roadmap { padding: 96px 0; position: relative; z-index: 2; }
.rm-grid { display: grid; grid-template-columns: 1.3fr 1fr; gap: 40px; }
.rm-timeline { display: flex; flex-direction: column; }
.rm-entry { display: grid; grid-template-columns: 28px 1fr; gap: 16px; padding: 20px 0; border-bottom: 1px dashed var(--line); }
.rm-entry:last-child { border-bottom: none; }
.rm-bullet { display: grid; place-items: center; padding-top: 6px; position: relative; }
.rm-bullet::before {
  content: ''; position: absolute; top: 24px; bottom: -20px;
  width: 1px; background: var(--line); left: 13px;
}
.rm-entry:last-child .rm-bullet::before { display: none; }
.rm-head { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
.rm-ver { font-weight: 700; font-size: 14px; color: #f5faf4; font-family: 'JetBrains Mono', monospace; }
.rm-notes { margin: 0; padding: 0 0 0 14px; font-size: 13px; color: var(--ink-dim); line-height: 1.7; }
.rm-notes li::marker { color: var(--phosphor); }
.rm-oss { padding: 28px 24px; align-self: start; }
.rm-oss-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px 12px; }
.rm-stat-v { font-family: 'JetBrains Mono', monospace; font-size: 24px; color: #f5faf4; font-weight: 600; }
.rm-activity { display: grid; grid-template-columns: repeat(26, 1fr); grid-auto-rows: 1fr; gap: 2px; aspect-ratio: 13 / 1; }
.rm-act-cell { aspect-ratio: 1; border-radius: 1px; }
@media (max-width: 900px) { .rm-grid { grid-template-columns: 1fr; } }

/* FAQ */
.faq { padding: 96px 0; position: relative; z-index: 2; }
.faq-list { border-top: 1px solid var(--line); }
.faq-item {
  width: 100%; display: block; text-align: left;
  background: transparent; border: none;
  border-bottom: 1px solid var(--line);
  padding: 0; color: inherit; font: inherit; cursor: pointer;
}
.faq-head { display: flex; align-items: center; gap: 20px; padding: 22px 4px; }
.faq-num { font-family: 'JetBrains Mono', monospace; font-size: 11px; color: var(--phosphor); letter-spacing: 0.1em; }
.faq-q { font-family: 'Inter Tight', sans-serif; font-size: 20px; font-weight: 500; color: #e9efe8; flex: 1; letter-spacing: -0.01em; }
.faq-icon { font-size: 22px; color: var(--ink-dim); font-weight: 300; width: 20px; text-align: center; }
.faq-item.open .faq-icon { color: var(--phosphor); }
.faq-item:hover .faq-q { color: #f5faf4; }
.faq-a {
  padding: 0 4px 24px 52px;
  font-family: 'Inter Tight', sans-serif;
  font-size: 15px; line-height: 1.65;
  color: var(--ink-dim); max-width: 780px;
}

/* Telegram demo */
.tg-frame {
  width: 380px; height: 560px;
  background: #14181e; border: 1px solid var(--line-hi);
  border-radius: 14px; display: flex; flex-direction: column;
  overflow: hidden;
  box-shadow: 0 30px 80px -20px rgba(0,0,0,0.8), var(--glow);
  font-family: 'Inter Tight', system-ui, sans-serif;
}
.tg-top {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 14px; background: #1c232b;
  border-bottom: 1px solid rgba(255,255,255,0.05);
}
.tg-avatar {
  width: 34px; height: 34px; border-radius: 50%;
  background: #0c1013; display: grid; place-items: center;
  border: 1px solid rgba(120,255,170,0.3);
  color: var(--phosphor); font-weight: 900; font-size: 14px;
}
.tg-body {
  flex: 1; padding: 14px 12px; overflow-y: auto;
  display: flex; flex-direction: column; gap: 4px;
  background: radial-gradient(ellipse at 30% 20%, rgba(120,255,170,0.04), transparent 60%), #0e1216;
}
.tg-body::-webkit-scrollbar { width: 3px; }
.tg-msg { display: flex; margin-bottom: 4px; }
.tg-msg.me { justify-content: flex-end; }
.tg-msg.bot { justify-content: flex-start; }
.tg-bubble {
  max-width: 82%; padding: 7px 11px 4px; border-radius: 12px;
  font-size: 12.5px; line-height: 1.45; position: relative;
}
.tg-msg.me .tg-bubble { background: linear-gradient(180deg, #2d7a4a, #1f5934); color: #f3f9f2; border-bottom-right-radius: 4px; }
.tg-msg.bot .tg-bubble { background: #1a2028; color: #d6dde0; border-bottom-left-radius: 4px; }
.tg-text { white-space: pre-wrap; }
.tg-time { font-size: 9.5px; color: rgba(255,255,255,0.4); text-align: right; margin-top: 2px; font-family: 'JetBrains Mono', monospace; }
.tg-bubble.typing { display: inline-flex; gap: 3px; padding: 10px 12px; }
.tg-bubble.typing span { width: 5px; height: 5px; border-radius: 50%; background: #7c8b82; animation: tg-dots 1.3s infinite; }
.tg-bubble.typing span:nth-child(2) { animation-delay: 0.15s; }
.tg-bubble.typing span:nth-child(3) { animation-delay: 0.3s; }
@keyframes tg-dots { 0%, 60%, 100% { opacity: 0.3; transform: translateY(0); } 30% { opacity: 1; transform: translateY(-3px); } }
.tg-input {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; background: #1c232b;
  border-top: 1px solid rgba(255,255,255,0.05); color: #7c8b82;
}
.tg-input-field { flex: 1; background: #0e1216; border-radius: 16px; padding: 8px 14px; font-size: 12px; }

/* ======== CONSOLE SPECIFIC ======== */
.console { padding: 32px 0 80px; position: relative; z-index: 2; min-height: 100vh; }
.page-head {
  display: flex; justify-content: space-between; align-items: flex-end;
  margin-bottom: 32px; gap: 20px;
}
.page-title {
  font-family: 'Inter Tight', sans-serif;
  font-size: 36px; font-weight: 700;
  letter-spacing: -0.025em; color: #f5faf4; margin: 0; line-height: 1;
}
.stat-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 12px; }
.tile {
  background: var(--panel); border: 1px solid var(--line);
  padding: 18px 20px; border-radius: var(--r-s);
}
.tile-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.tile-num {
  font-family: 'JetBrains Mono', monospace;
  font-size: 34px; font-weight: 500; letter-spacing: -0.02em;
  color: #f5faf4; line-height: 1;
}
.tile-unit { font-size: 16px; color: var(--ink-faint); margin-left: 4px; }
.tile-foot { display: flex; justify-content: space-between; align-items: flex-end; margin-top: 10px; }
.sub-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 32px; }
.sub-tile {
  padding: 14px 16px; background: var(--bg-1);
  border: 1px solid var(--line); border-radius: var(--r-s);
}
.sub-val { font-family: 'JetBrains Mono', monospace; font-size: 15px; color: #e9efe8; margin: 4px 0 3px; font-weight: 500; }
.cons-grid { display: grid; grid-template-columns: 1fr 320px; gap: 20px; }
.sessions {
  background: var(--panel); border: 1px solid var(--line);
  border-radius: var(--r-s); overflow: hidden;
}
.panel-head {
  padding: 14px 18px; display: flex; justify-content: space-between;
  align-items: center; border-bottom: 1px solid var(--line);
}
.feed { max-height: 360px; overflow-y: auto; padding: 6px 0; }
.feed-row {
  display: grid; grid-template-columns: 70px 34px 70px 110px 1fr;
  gap: 12px; padding: 7px 18px; font-size: 12px;
  align-items: center; border-bottom: 1px dashed var(--line);
}
.feed-row:last-child { border-bottom: none; }
.feed-row:hover { background: rgba(120,255,170,0.02); }
.feed-t { font-family: 'JetBrains Mono', monospace; color: var(--ink-faint); font-size: 11px; }
.feed-lvl { font-family: 'JetBrains Mono', monospace; font-weight: 600; font-size: 11px; text-align: center; padding: 2px 0; }
.feed-inf { color: var(--cyan); }
.feed-ok { color: var(--phosphor); }
.feed-wrn { color: var(--amber); }
.feed-err { color: var(--red); }
.feed-tag { justify-self: start; font-size: 9.5px !important; }
.tag-bash { color: var(--phosphor); border-color: rgba(120,255,170,0.3); }
.tag-edit { color: var(--amber); border-color: rgba(255,180,70,0.3); }
.tag-read { color: var(--cyan); border-color: rgba(130,210,255,0.3); }
.feed-from { color: var(--ink); font-family: 'JetBrains Mono', monospace; font-size: 11.5px; }
.feed-text { font-size: 12.5px; color: var(--ink); }
.logtail {
  margin: 0; padding: 14px 18px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 11.5px; line-height: 1.7;
  color: #8fb096; white-space: pre-wrap; overflow-x: auto;
  max-height: 300px;
}
.log-t { color: #4a6658; }
.log-inf { color: var(--cyan); font-weight: 600; }
.log-ok { color: var(--phosphor); font-weight: 600; }
.log-wrn { color: var(--amber); font-weight: 600; }
.rail { display: flex; flex-direction: column; gap: 12px; }
.rail-card {
  background: var(--panel); border: 1px solid var(--line);
  border-radius: var(--r-s); padding: 16px;
}
.op-list { display: flex; flex-direction: column; gap: 10px; margin: 10px 0 4px; }
.op-row { display: flex; align-items: center; gap: 10px; padding: 4px 0; }
.op-av {
  width: 28px; height: 28px; border-radius: 3px;
  display: grid; place-items: center;
  background: var(--bg-1); border: 1px solid var(--line-hi);
  font-family: 'JetBrains Mono', monospace;
  font-size: 10.5px; font-weight: 700; color: var(--phosphor);
}
.ws-bar { height: 4px; background: var(--bg-1); border-radius: 2px; overflow: hidden; margin-top: 12px; }
.ws-fill { height: 100%; background: var(--phosphor); box-shadow: 0 0 8px var(--phosphor); }
.ws-files { display: flex; flex-direction: column; gap: 8px; }
.ws-file { display: flex; align-items: center; gap: 8px; color: var(--ink-dim); }
.ws-file:hover { color: var(--phosphor); cursor: pointer; }
.qa-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.qa {
  padding: 10px 12px; background: var(--bg-1);
  border: 1px solid var(--line); color: var(--ink-dim);
  font-family: inherit; font-size: 11px;
  letter-spacing: 0.06em; text-transform: uppercase;
  text-align: left; cursor: pointer; border-radius: var(--r-s);
  display: flex; align-items: center; gap: 6px; transition: all 0.1s;
}
.qa:hover { color: var(--phosphor); border-color: var(--phosphor); background: var(--phosphor-wash); }

/* Login page */
.login-wrap { min-height: 100vh; display: grid; place-items: center; position: relative; z-index: 2; }

/* Direction indicators for events */
.dir-in { color: var(--cyan); font-weight: 600; }
.dir-out { color: var(--phosphor); font-weight: 600; }
.dir-error { color: var(--red); font-weight: 600; }

@media (max-width: 1100px) {
  .stat-row, .sub-row { grid-template-columns: repeat(2, 1fr); }
  .cons-grid { grid-template-columns: 1fr; }
}
`

const publicHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw · self-hosted agent orchestration</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#05070a">
<meta name="description" content="Self-hosted agent orchestration for teams who'd rather run Claude Code on their own metal. Spin up sandboxed sessions per operator, reach them from Telegram, keep every log on a box you own.">
<style>{{.CSS}}</style>
</head><body>

<!-- ====== NAV ====== -->
<header class="gnav">
  <div class="container gnav-inner">
    <a href="/" class="claw-logo">
      <span class="mark">&#x276F;</span>
      <span>OPENCLAW</span>
      <span style="color:var(--ink-faint);font-weight:400;margin-left:2px">v2026.4</span>
    </a>
    <nav class="gnav-links">
      <a class="active" href="#product">Product</a>
      <a href="#security">Security</a>
      <a href="#roadmap">Roadmap</a>
      <a href="https://github.com/anchoo2kewl/openclaw">Docs</a>
    </nav>
    <div class="gnav-right">
      <a href="https://github.com/anchoo2kewl/openclaw" target="_blank" rel="noreferrer" class="btn btn-ghost" style="padding:4px 10px">
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
        github
      </a>
      <a href="/login" class="btn" style="padding:6px 12px">Sign in</a>
      <a href="#install" class="btn btn-primary" style="padding:6px 12px">Install &#x2192;</a>
    </div>
  </div>
</header>

<!-- ====== HERO ====== -->
<section class="hero" id="product">
  <div class="container hero-inner">
    <div class="hero-left">
      <div class="kicker" style="margin-bottom:24px">
        <span class="dot ok" style="margin-right:8px"></span> OPENCLAW · v2026.4 · <span id="hero-clock">00:00:00</span> UTC
      </div>
      <h1 class="hero-title prose">
        Your operators.<br>
        Your VM.<br>
        <span class="hero-hl">One command away.</span>
      </h1>
      <p class="hero-sub prose">
        Openclaw is self-hosted agent orchestration for teams who'd rather run Claude Code on their own
        metal than hand over a shell. Spin up sandboxed sessions per operator, reach them from
        Telegram, and keep every log on a box you own.
      </p>
      <div class="hero-cta">
        <a href="#install" class="btn btn-primary">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h12v10H2zM4 6l2 2-2 2M8 10h4"/></svg>
          curl install
        </a>
        <a href="/login" class="btn">
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 2v12l10-6z"/></svg>
          View console demo
        </a>
        <a href="https://github.com/anchoo2kewl/openclaw" target="_blank" rel="noreferrer" class="btn btn-ghost">
          <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
          <span style="margin-left:4px">anchoo2kewl/openclaw</span>
        </a>
      </div>
      <div class="hero-stats corner">
        <div class="hs-cell">
          <div class="eyebrow">sessions / day</div>
          <div class="hs-num numeric">1,284</div>
          <div class="hs-delta">+12.4% w/w</div>
        </div>
        <div class="hs-cell">
          <div class="eyebrow">self-hosted nodes</div>
          <div class="hs-num numeric">207</div>
          <div class="hs-delta">across 14 orgs</div>
        </div>
        <div class="hs-cell">
          <div class="eyebrow">avg p95 cold start</div>
          <div class="hs-num numeric">1.8<span style="font-size:0.5em;color:var(--ink-dim)">s</span></div>
          <div class="hs-delta">container warm pool</div>
        </div>
        <div class="hs-cell">
          <div class="eyebrow">uptime · 90d</div>
          <div class="hs-num numeric">99.97%</div>
          <div class="hs-delta"><span class="dot ok"></span> all systems nominal</div>
        </div>
      </div>
    </div>

    <div class="hero-right">
      <div class="hero-right-label">
        <span class="kicker">LIVE · session #a7f3</span>
        <span class="chip chip-green"><span class="dot ok pulse"></span> streaming</span>
      </div>
      <!-- Telegram demo -->
      <div class="tg-frame" id="tg-demo">
        <div class="tg-top">
          <div style="display:flex;align-items:center;gap:10px">
            <div class="tg-avatar">&#x276F;</div>
            <div>
              <div style="font-weight:600;font-size:13px;color:#e9efe8">clawdy</div>
              <div style="font-size:10.5px;color:#7c8b82;font-family:'JetBrains Mono'">
                <span class="dot ok"></span> online · session #a7f3
              </div>
            </div>
          </div>
          <div style="display:flex;gap:14px;color:#7c8b82">
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M7 13a6 6 0 1 1 0-12 6 6 0 0 1 0 12zM11 11l3 3"/></svg>
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 4h12M2 8h12M2 12h12"/></svg>
          </div>
        </div>
        <div class="tg-body" id="tg-body"></div>
        <div class="tg-input">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3v10M3 8h10"/></svg>
          <div class="tg-input-field">Message</div>
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 8l13-6-5 13-3-5-5-2z"/></svg>
        </div>
      </div>
      <div class="hero-right-caption prose ink-dim">
        This is a real session running on a single 4-CPU VM.
        <br>Every bubble above is a Claude Code tool-call.
      </div>
    </div>
  </div>
</section>

<hr class="divider">

<!-- ====== ARCHITECTURE ====== -->
<section class="arch" id="topology">
  <div class="container">
    <div class="section-head">
      <div>
        <div class="kicker">&#xA7;02 · TOPOLOGY</div>
        <h2 class="section-title prose">One VM. Zero vendor lock-in.</h2>
      </div>
      <p class="section-lede prose ink-dim">
        Telegram long-polls your bot. Your bot shells into an ephemeral Claude Code
        container. Nginx never sees inbound traffic for the bot path &mdash; it's just there
        for health. Everything lives in a bind mount you control.
      </p>
    </div>

    <div class="arch-diagram panel corner">
      <div class="arch-row" id="arch-row">
        <div class="arch-node on" data-idx="0">
          <div class="arch-node-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 8l13-6-5 13-3-5-5-2z"/></svg></div>
          <div class="arch-node-label">Telegram</div>
          <div class="arch-node-sub">HTTPS long-poll</div>
        </div>
        <div class="arch-edge"><div class="arch-edge-line on"></div><div class="arch-edge-label">https</div></div>
        <div class="arch-node" data-idx="1">
          <div class="arch-node-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 1l6 2v4c0 4-2.5 7-6 8-3.5-1-6-4-6-8V3l6-2z"/></svg></div>
          <div class="arch-node-label">Bot</div>
          <div class="arch-node-sub">python · allowlist</div>
        </div>
        <div class="arch-edge"><div class="arch-edge-line"></div><div class="arch-edge-label">exec</div></div>
        <div class="arch-node" data-idx="2">
          <div class="arch-node-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h12v10H2zM4 6l2 2-2 2M8 10h4"/></svg></div>
          <div class="arch-node-label">Claude Code</div>
          <div class="arch-node-sub">docker · /workspace</div>
        </div>
        <div class="arch-edge"><div class="arch-edge-line"></div><div class="arch-edge-label">bind</div></div>
        <div class="arch-node" data-idx="3">
          <div class="arch-node-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 2h12v5H2zM2 9h12v5H2zM5 5h.01M5 12h.01"/></svg></div>
          <div class="arch-node-label">Your VM</div>
          <div class="arch-node-sub">nginx · cloudflare</div>
        </div>
      </div>

      <div class="arch-details">
        <div><div class="eyebrow">outbound only</div><div class="ink-dim" style="font-size:12px;margin-top:4px">No inbound port needed for the bot itself.</div></div>
        <div><div class="eyebrow">UFW · 22/80/443</div><div class="ink-dim" style="font-size:12px;margin-top:4px">Fail2ban on sshd, CF-proxied DNS.</div></div>
        <div><div class="eyebrow">workspace volume</div><div class="ink-dim" style="font-size:12px;margin-top:4px">/opt/openclaw/workspace, host-owned.</div></div>
        <div><div class="eyebrow">secrets · mode 600</div><div class="ink-dim" style="font-size:12px;margin-top:4px">Never in the repo. Never in the image.</div></div>
      </div>
    </div>
  </div>
</section>

<hr class="divider">

<!-- ====== SECURITY ====== -->
<section class="sec" id="security">
  <div class="container">
    <div class="section-head">
      <div>
        <div class="kicker">&#xA7;03 · POSTURE</div>
        <h2 class="section-title prose">Security that a paranoid SRE won't sigh at.</h2>
      </div>
      <p class="section-lede prose ink-dim">
        Openclaw runs Claude Code in YOLO mode &mdash; on purpose. That's the whole value prop.
        So the rest of the stack is hardened to compensate: no inbound surface for the agent,
        container-per-session, everything on your box.
      </p>
    </div>
    <div class="sec-grid">
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 1l6 2v4c0 4-2.5 7-6 8-3.5-1-6-4-6-8V3l6-2z"/></svg></div><span class="eyebrow">CTRL · A01</span></div><div class="sec-card-title prose">Telegram allowlist</div><div class="sec-card-desc prose ink-dim">Numeric user IDs only. Rejects before exec. Zero public surface.</div></div>
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 4l7 3 7-3M8 7v8M1 4l7-3 7 3v8l-7 3-7-3V4z"/></svg></div><span class="eyebrow">CTRL · A02</span></div><div class="sec-card-title prose">Docker sandbox</div><div class="sec-card-desc prose ink-dim">Every session runs in an ephemeral container. Workspace is the only mount.</div></div>
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 6a3 3 0 1 0-2.83 3l1.33 1.33L10 9l-1 1 1 1-1 1 2 2 3-3-4.17-4.17c.1-.27.17-.54.17-.83z"/></svg></div><span class="eyebrow">CTRL · A03</span></div><div class="sec-card-title prose">Secrets · mode 600</div><div class="sec-card-desc prose ink-dim">/opt/openclaw/.env owned by root. Never committed, never baked into images.</div></div>
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7V5a4 4 0 1 1 8 0v2M3 7h10v8H3z"/></svg></div><span class="eyebrow">CTRL · A04</span></div><div class="sec-card-title prose">Cloudflare origin CA</div><div class="sec-card-desc prose ink-dim">15-year origin cert. UFW allows 22/80/443 only. Fail2ban on sshd.</div></div>
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 2h12v5H2zM2 9h12v5H2zM5 5h.01M5 12h.01"/></svg></div><span class="eyebrow">CTRL · A05</span></div><div class="sec-card-title prose">Your data, your box</div><div class="sec-card-desc prose ink-dim">Logs, workspace, conversation history &mdash; all on disk you own. No telemetry.</div></div>
      <div class="sec-card"><div class="sec-card-head"><div class="sec-card-icon"><svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 8s3-5 7-5 7 5 7 5-3 5-7 5-7-5-7-5zM8 10.5A2.5 2.5 0 1 0 8 5.5a2.5 2.5 0 0 0 0 5z"/></svg></div><span class="eyebrow">CTRL · A06</span></div><div class="sec-card-title prose">Audit trail</div><div class="sec-card-desc prose ink-dim">Every tool call is journaled. Stream to stdout, syslog, or your SIEM.</div></div>
    </div>
  </div>
</section>

<hr class="divider">

<!-- ====== INSTALL ====== -->
<section class="install" id="install">
  <div class="container">
    <div class="install-wrap panel-hi corner">
      <div class="install-left">
        <div class="kicker">&#xA7;04 · INSTALL</div>
        <h2 class="section-title prose" style="margin-top:8px">One command. One VM. Five minutes.</h2>
        <p class="prose ink-dim" style="font-size:15px;line-height:1.6;max-width:460px">
          Bootstrap issues a Cloudflare origin cert, hardens the box via Ansible,
          deploys the bot container, and wires up nginx. You bring a Telegram token and
          your Anthropic key.
        </p>
        <div style="display:flex;gap:12px;margin-top:24px">
          <a href="https://github.com/anchoo2kewl/openclaw" class="btn btn-primary">
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3a2 2 0 0 1 2-2h4v12H4a2 2 0 0 0-2 2V3zM14 3a2 2 0 0 0-2-2H8v12h4a2 2 0 0 1 2 2V3z"/></svg>
            Read the docs
          </a>
          <a href="https://github.com/anchoo2kewl/openclaw" target="_blank" rel="noreferrer" class="btn">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
            <span style="margin-left:4px">Source</span>
          </a>
        </div>
      </div>
      <div class="install-right">
        <div class="terminal">
          <div class="terminal-top">
            <span class="tt-dot r"></span><span class="tt-dot y"></span><span class="tt-dot g"></span>
            <span style="margin-left:12px;font-size:11px;color:var(--ink-faint)">~/ops &mdash; zsh</span>
            <button class="tt-copy" id="copy-btn" onclick="navigator.clipboard&&navigator.clipboard.writeText('curl -fsSL openclaw.dev/install.sh | bash');var b=this;b.textContent='copied';setTimeout(function(){b.textContent='copy'},1400)">copy</button>
          </div>
          <pre class="terminal-body"><span class="cm"># 1. bootstrap a fresh ubuntu 22.04 vm</span>
<span class="tp">$</span> curl -fsSL openclaw.dev/install.sh | bash

<span class="cm"># 2. paste your secrets when prompted</span>
<span class="tp">?</span> TELEGRAM_BOT_TOKEN   <span class="str">********</span>
<span class="tp">?</span> TELEGRAM_ALLOWED_IDS <span class="str">8417342395</span>
<span class="tp">?</span> ANTHROPIC_API_KEY    <span class="str">sk-ant-********</span>

<span class="ok">&#x2713;</span> CF dns record created
<span class="ok">&#x2713;</span> origin cert issued (15yr)
<span class="ok">&#x2713;</span> ansible playbook green (22 tasks)
<span class="ok">&#x2713;</span> openclaw online on <span class="str">claw.biswas.me</span>

<span class="cm"># DM @clawdy on telegram to begin.</span>
<span class="tp">$</span> <span class="cursor"></span></pre>
        </div>
      </div>
    </div>
  </div>
</section>

<hr class="divider">

<!-- ====== ROADMAP ====== -->
<section class="roadmap" id="roadmap">
  <div class="container">
    <div class="section-head">
      <div>
        <div class="kicker">&#xA7;05 · TRAJECTORY</div>
        <h2 class="section-title prose">Shipping weekly. Public forever.</h2>
      </div>
      <p class="section-lede prose ink-dim">
        Every release cuts from <span class="mono" style="color:var(--phosphor)">main</span>, tagged and signed.
        The roadmap lives in the same repo as the code.
      </p>
    </div>
    <div class="rm-grid">
      <div class="rm-timeline">
        <div class="rm-entry">
          <div class="rm-bullet"><span class="dot ok pulse"></span></div>
          <div>
            <div class="rm-head"><span class="rm-ver">v2026.4.15</span><span class="chip chip-green">current</span><span class="ink-faint" style="margin-left:auto;font-size:11px">Apr 15, 2026</span></div>
            <ul class="rm-notes"><li>Multi-operator allowlist</li><li>Activity ring buffer · 200 events</li><li>Origin CA auto-rotation</li></ul>
          </div>
        </div>
        <div class="rm-entry">
          <div class="rm-bullet"><span class="dot warn"></span></div>
          <div>
            <div class="rm-head"><span class="rm-ver">v2026.5.0</span><span class="chip chip-amber">next</span><span class="ink-faint" style="margin-left:auto;font-size:11px">May 2026</span></div>
            <ul class="rm-notes"><li>Gateway SSO (OIDC)</li><li>Per-operator workspace quotas</li><li>Slack + Discord adapters</li></ul>
          </div>
        </div>
        <div class="rm-entry">
          <div class="rm-bullet"><span class="dot idle"></span></div>
          <div>
            <div class="rm-head"><span class="rm-ver">v2026.6.0</span><span class="chip">planned</span><span class="ink-faint" style="margin-left:auto;font-size:11px">Jun 2026</span></div>
            <ul class="rm-notes"><li>Multi-VM fleet mode</li><li>Policy-as-code (OPA)</li><li>Managed-cloud beta</li></ul>
          </div>
        </div>
        <div class="rm-entry">
          <div class="rm-bullet"><span class="dot idle"></span></div>
          <div>
            <div class="rm-head"><span class="rm-ver">v2026.q4</span><span class="chip">planned</span><span class="ink-faint" style="margin-left:auto;font-size:11px">Q4 2026</span></div>
            <ul class="rm-notes"><li>SOC 2 Type I</li><li>BYO-model runtime (vLLM, Ollama)</li><li>Audit export &#x2192; Splunk/Loki</li></ul>
          </div>
        </div>
      </div>
      <div class="rm-oss panel corner">
        <div class="kicker" style="margin-bottom:16px">OSS · 90d</div>
        <div class="rm-oss-grid">
          <div class="rm-stat"><div class="rm-stat-v numeric">42</div><div class="eyebrow">weekly commits</div></div>
          <div class="rm-stat"><div class="rm-stat-v numeric">17</div><div class="eyebrow">issues · open</div></div>
          <div class="rm-stat"><div class="rm-stat-v numeric">213</div><div class="eyebrow">issues · closed</div></div>
          <div class="rm-stat"><div class="rm-stat-v numeric">11</div><div class="eyebrow">contributors</div></div>
          <div class="rm-stat"><div class="rm-stat-v numeric">+1.4k</div><div class="eyebrow">stars · 90d</div></div>
          <div class="rm-stat"><div class="rm-stat-v numeric">86</div><div class="eyebrow">forks</div></div>
        </div>
        <hr class="divider-dashed" style="margin:20px 0">
        <div class="rm-activity" id="rm-heatmap"></div>
        <div class="eyebrow" style="margin-top:8px;text-align:right">commit density · last 52w</div>
      </div>
    </div>
  </div>
</section>

<hr class="divider">

<!-- ====== FAQ ====== -->
<section class="faq" id="faq">
  <div class="container">
    <div class="section-head">
      <div>
        <div class="kicker">&#xA7;06 · FAQ</div>
        <h2 class="section-title prose">Things everyone asks.</h2>
      </div>
    </div>
    <div class="faq-list" id="faq-list">
      <button class="faq-item open" data-idx="0"><div class="faq-head"><span class="faq-num">01</span><span class="faq-q">Do I need a dedicated VM?</span><span class="faq-icon">&minus;</span></div><div class="faq-a flash-in">A 2-CPU / 4GB ubuntu box is plenty. The bot is tiny; Claude Code runs in a container spawned per session. A $6/mo VPS has handled 600+ sessions a day for our main deployment.</div></button>
      <button class="faq-item" data-idx="1"><div class="faq-head"><span class="faq-num">02</span><span class="faq-q">Why Telegram?</span><span class="faq-icon">+</span></div><div class="faq-a" style="display:none">Because it's outbound-only. No inbound webhook, no public port, no Cloudflare Zero Trust tunnel. Your bot long-polls api.telegram.org &mdash; that's the whole ingress story. Other adapters (Slack, Discord) are on the roadmap.</div></button>
      <button class="faq-item" data-idx="2"><div class="faq-head"><span class="faq-num">03</span><span class="faq-q">Is 'YOLO mode' safe?</span><span class="faq-icon">+</span></div><div class="faq-a" style="display:none">No, and that's the point. --dangerously-skip-permissions means Claude runs any tool without asking. The compensations: every session is a fresh container, the allowlist pins who can invoke it, and the workspace is the only writable mount.</div></button>
      <button class="faq-item" data-idx="3"><div class="faq-head"><span class="faq-num">04</span><span class="faq-q">Can I self-host on bare metal?</span><span class="faq-icon">+</span></div><div class="faq-a" style="display:none">Yes. The Ansible role targets Ubuntu 22.04 LTS. We test on Hetzner, DigitalOcean, Tencent Cloud Lighthouse, and a ThinkCentre in a closet. There is no "cloud" requirement.</div></button>
      <button class="faq-item" data-idx="4"><div class="faq-head"><span class="faq-num">05</span><span class="faq-q">What's the license?</span><span class="faq-icon">+</span></div><div class="faq-a" style="display:none">MIT. You can fork it, rebrand it, sell it, host it for your team. If you build something cool, open a PR &mdash; we love merging them.</div></button>
      <button class="faq-item" data-idx="5"><div class="faq-head"><span class="faq-num">06</span><span class="faq-q">Is a managed cloud coming?</span><span class="faq-icon">+</span></div><div class="faq-a" style="display:none">Yes, in beta Q3. Same code, we run the VM. Same price-performance as a $6 VPS, just without the ansible step. The self-hosted flavor stays free and first-class.</div></button>
    </div>
  </div>
</section>

<!-- ====== FOOTER ====== -->
<footer class="foot">
  <div class="container">
    <div class="foot-top">
      <div>
        <div class="claw-logo" style="margin-bottom:16px">
          <span class="mark">&#x276F;</span>
          <span>OPENCLAW</span>
        </div>
        <div class="prose ink-dim" style="max-width:320px;font-size:13px;line-height:1.55">
          Self-hosted agent orchestration. Telegram-driven Claude Code. Your VM, your rules.
        </div>
        <div style="margin-top:24px;display:flex;gap:8px">
          <a class="btn" href="https://github.com/anchoo2kewl/openclaw" target="_blank" rel="noreferrer">
            <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>
            <span style="margin-left:4px">Star on GitHub</span>
          </a>
        </div>
      </div>
      <div class="foot-cols">
        <div><div class="eyebrow" style="margin-bottom:12px">product</div><ul class="foot-list"><li><a href="#">Overview</a></li><li><a href="/login">Console</a></li><li><a href="#">Gateway</a></li><li><a href="#">Changelog</a></li></ul></div>
        <div><div class="eyebrow" style="margin-bottom:12px">developers</div><ul class="foot-list"><li><a href="https://github.com/anchoo2kewl/openclaw">Docs</a></li><li><a href="#">API reference</a></li><li><a href="#">Ansible roles</a></li><li><a href="#security">Security</a></li></ul></div>
        <div><div class="eyebrow" style="margin-bottom:12px">community</div><ul class="foot-list"><li><a href="https://github.com/anchoo2kewl/openclaw">GitHub</a></li><li><a href="#">Discord</a></li><li><a href="#">Twitter</a></li><li><a href="#">Contributing</a></li></ul></div>
        <div><div class="eyebrow" style="margin-bottom:12px">legal</div><ul class="foot-list"><li><a href="#">MIT license</a></li><li><a href="#">Privacy</a></li><li><a href="#">Trademarks</a></li></ul></div>
      </div>
    </div>
    <div class="foot-bot">
      <span>&copy; 2026 openclaw · MIT · <span class="ink-faint">claw.biswas.me</span></span>
      <span class="ink-faint">&#x22EF; this page served from a single 4-CPU VM in YYZ1 &#x22EF;</span>
      <span><span class="dot ok"></span> all systems nominal</span>
    </div>
  </div>
</footer>

<script>
// Live clock
setInterval(function(){
  var d=new Date(),el=document.getElementById('hero-clock');
  if(el)el.textContent=[d.getUTCHours(),d.getUTCMinutes(),d.getUTCSeconds()].map(function(v){return String(v).padStart(2,'0')}).join(':');
},1000);

// Architecture animation
(function(){
  var nodes=document.querySelectorAll('.arch-node'),edges=document.querySelectorAll('.arch-edge-line'),active=0;
  setInterval(function(){
    nodes.forEach(function(n){n.classList.remove('on')});
    edges.forEach(function(e){e.classList.remove('on')});
    active=(active+1)%4;
    nodes[active].classList.add('on');
    if(active>0)edges[active-1].classList.add('on');
  },2000);
})();

// Telegram demo
(function(){
  var script=[
    {side:'me',text:'tail the nginx error log on prod. find anything spiking in the last hour.',delay:0},
    {side:'bot',typing:800,text:'Running on prod-01 via /workspace/ops \u2014 claude-sonnet-4-5, session #a7f3.',delay:500},
    {side:'bot',text:'Found 2,104 new errors in 62m.\n\u2022 1,903\u00D7 upstream timeout from svc=payment-api\n\u2022 201\u00D7 502 from svc=search\nPayment-api stopped returning /healthz at 22:41.',delay:1600},
    {side:'me',text:'roll back payment-api to v8.12.1. page no one.',delay:900},
    {side:'bot',typing:1200,text:'Rolling back\u2026\n$ kubectl rollout undo deploy/payment-api --to-revision=41\ndeployment.apps/payment-api rolled back',delay:600},
    {side:'bot',text:'\u2713 payment-api healthy in 22s. Error rate dropping: 1,903/m \u2192 12/m \u2192 0/m. Want a post-mortem stub committed to ops/incidents?',delay:1400},
    {side:'me',text:'yes, assign to @a. link the grafana range.',delay:700}
  ];
  var body=document.getElementById('tg-body'),step=0;
  function addMsg(m){
    var d=document.createElement('div');d.className='tg-msg '+m.side+' flash-in';
    var b=document.createElement('div');b.className='tg-bubble';
    var t=document.createElement('div');t.className='tg-text';t.textContent=m.text;
    var tm=document.createElement('div');tm.className='tg-time';
    tm.innerHTML=(step%2===0?'22:41':'22:42')+(m.side==='me'?' <span style="color:#6fbfff">\u2713\u2713</span>':'');
    b.appendChild(t);b.appendChild(tm);d.appendChild(b);body.appendChild(d);
    body.scrollTop=body.scrollHeight;
  }
  function showTyping(){
    var d=document.createElement('div');d.className='tg-msg bot flash-in';d.id='typing-ind';
    var b=document.createElement('div');b.className='tg-bubble typing';
    b.innerHTML='<span></span><span></span><span></span>';
    d.appendChild(b);body.appendChild(d);body.scrollTop=body.scrollHeight;
  }
  function removeTyping(){var el=document.getElementById('typing-ind');if(el)el.remove();}
  function next(){
    if(step>=script.length){setTimeout(function(){body.innerHTML='';step=0;next();},5000);return;}
    var m=script[step],total=(m.typing||0)+(m.delay||0)+(m.side==='bot'?600:400);
    if(m.typing)showTyping();
    setTimeout(function(){
      removeTyping();addMsg(m);step++;next();
    },total);
  }
  next();
})();

// FAQ accordion
document.getElementById('faq-list').addEventListener('click',function(e){
  var item=e.target.closest('.faq-item');if(!item)return;
  var items=document.querySelectorAll('.faq-item');
  var wasOpen=item.classList.contains('open');
  items.forEach(function(it){
    it.classList.remove('open');
    var a=it.querySelector('.faq-a'),ic=it.querySelector('.faq-icon');
    if(a)a.style.display='none';
    if(ic)ic.textContent='+';
  });
  if(!wasOpen){
    item.classList.add('open');
    var a=item.querySelector('.faq-a'),ic=item.querySelector('.faq-icon');
    if(a){a.style.display='block';a.classList.add('flash-in');}
    if(ic)ic.textContent='\u2212';
  }
});

// Heatmap
(function(){
  var hm=document.getElementById('rm-heatmap');if(!hm)return;
  var colors=['#10181a','#1a3a2a','#2a6a4a','#4aaa6a','#7affbf'];
  for(var i=0;i<52;i++){var c=document.createElement('div');c.className='rm-act-cell';c.style.background=colors[Math.floor(Math.random()*5)];hm.appendChild(c);}
})();
</script>
</body></html>`

const dashboardHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<meta http-equiv=refresh content=15>
<title>openclaw · console</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#05070a">
<style>{{.CSS}}</style>
</head><body>

<!-- ====== NAV ====== -->
<header class="gnav">
  <div class="container gnav-inner">
    <a href="/" class="claw-logo">
      <span class="mark">&#x276F;</span>
      <span>OPENCLAW</span>
      <span style="color:var(--ink-faint);font-weight:400;margin-left:2px">v2026.4</span>
    </a>
    <nav class="gnav-links">
      <a class="active" href="/">Overview</a>
      <a href="#activity">Activity</a>
      <a href="#accounts">Accounts</a>
      <a href="#logs">Logs</a>
      {{if .HasGateway}}<a href="/gateway-launch">Gateway &#x2197;</a>{{end}}
    </nav>
    <div class="gnav-right">
      <span><span class="dot ok"></span> {{.Email}}</span>
      <form method="POST" action="/logout" style="margin:0">
        <button class="btn btn-ghost" type="submit" style="padding:4px 10px">Log out</button>
      </form>
    </div>
  </div>
</header>

<div class="console">
  <div class="container">

    <!-- ====== PAGE HEAD ====== -->
    <div class="page-head">
      <div>
        <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
          <span class="eyebrow">OPENCLAW · prod · yyz1</span>
          <span class="chip chip-green"><span class="dot ok pulse"></span> online</span>
          <span class="ink-faint" style="font-size:11px">uptime {{.Uptime}} · refresh every 15s</span>
        </div>
        <h1 class="page-title prose">Operator console</h1>
      </div>
      <div style="display:flex;gap:8px;align-items:center">
        {{if .HasGateway}}<a href="/gateway-launch" class="btn btn-primary">
          Open gateway <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 2h4v4M14 2L7 9M12 8v6H2V4h6"/></svg>
        </a>{{end}}
      </div>
    </div>

    <!-- ====== STAT ROW ====== -->
    <div class="stat-row">
      <div class="tile corner">
        <div class="tile-head"><span class="eyebrow">ACTIVE SESSIONS</span></div>
        <div class="tile-num numeric">{{len .Sessions}}</div>
        <div class="tile-foot"><span class="ink-faint" style="font-size:11px">Telegram conversations</span></div>
      </div>
      <div class="tile corner">
        <div class="tile-head"><span class="eyebrow">MESSAGES · LAST 200</span></div>
        <div class="tile-num numeric">{{len .Events}}</div>
        <div class="tile-foot"><span class="ink-faint" style="font-size:11px">ring-buffered events</span></div>
      </div>
      <div class="tile corner">
        <div class="tile-head"><span class="eyebrow">OPERATORS</span></div>
        <div class="tile-num numeric">{{len .Users}}</div>
        <div class="tile-foot"><span class="ink-faint" style="font-size:11px">dashboard accounts</span></div>
      </div>
      <div class="tile corner">
        <div class="tile-head"><span class="eyebrow">ALLOWLIST</span></div>
        <div class="tile-num numeric">{{len .Allowed}}</div>
        <div class="tile-foot"><span class="ink-faint" style="font-size:11px">Telegram IDs authorized</span></div>
      </div>
    </div>

    <!-- ====== SUB ROW ====== -->
    <div class="sub-row">
      <div class="sub-tile">
        <span class="eyebrow">model</span>
        <div class="sub-val mono">{{.Model}}</div>
        <div class="ink-faint" style="font-size:11px">provider · anthropic · api v1</div>
      </div>
      <div class="sub-tile">
        <span class="eyebrow">workspace</span>
        <div class="sub-val mono" style="word-break:break-all">{{.Workspace}}</div>
        <div class="ink-faint" style="font-size:11px">bind mount · host-owned</div>
      </div>
      <div class="sub-tile">
        <span class="eyebrow">host</span>
        <div class="sub-val mono">claw.biswas.me</div>
        <div class="ink-faint" style="font-size:11px">ubuntu 22.04 · yyz1</div>
      </div>
      <div class="sub-tile">
        <span class="eyebrow">allowed ids</span>
        <div class="sub-val mono" style="word-break:break-all">{{if .Allowed}}{{range $i, $u := .Allowed}}{{if $i}}, {{end}}{{$u}}{{end}}{{else}}&mdash;{{end}}</div>
        <div class="ink-faint" style="font-size:11px">Telegram user IDs</div>
      </div>
    </div>

    <!-- ====== MAIN GRID ====== -->
    <div class="cons-grid">
      <div style="display:flex;flex-direction:column;gap:20px">

        <!-- SESSIONS TABLE -->
        <div class="sessions corner" id="sessions">
          <div class="panel-head">
            <div>
              <span class="kicker">LIVE SESSIONS</span>
              <span class="ink-faint" style="margin-left:12px;font-size:11px">{{len .Sessions}} active</span>
            </div>
          </div>
          {{if .Sessions}}
          <table>
            <thead><tr><th>user</th><th>session id</th><th>workspace</th></tr></thead>
            <tbody>
            {{range .Sessions}}<tr>
              <td><span style="color:var(--phosphor)">{{.UserID}}</span></td>
              <td class="mono ink-dim" style="font-size:12px">{{if .SessionID}}#{{.SessionID}}{{else}}&mdash;{{end}}</td>
              <td class="mono" style="font-size:12px">{{.Cwd}}</td>
            </tr>{{end}}
            </tbody>
          </table>
          {{else}}
          <div style="padding:28px;text-align:center" class="ink-faint">No active sessions. A session is created when an allowed user sends their first message.</div>
          {{end}}
        </div>

        <!-- ACTIVITY FEED -->
        <div class="sessions corner" id="activity">
          <div class="panel-head">
            <div>
              <span class="kicker">ACTIVITY</span>
              <span class="ink-faint" style="margin-left:12px;font-size:11px">last 200 events · ring buffer</span>
            </div>
            <span class="chip chip-green"><span class="dot ok pulse"></span> live</span>
          </div>
          {{if .Events}}
          <div class="feed">
            {{range .Events}}<div class="feed-row">
              <span class="feed-t">{{fmtTime .Time}}</span>
              <span class="feed-lvl {{if eq .Direction "in"}}feed-inf{{else if eq .Direction "out"}}feed-ok{{else}}feed-wrn{{end}}">{{if eq .Direction "in"}}INF{{else if eq .Direction "out"}}OUT{{else}}ERR{{end}}</span>
              <span class="chip feed-tag {{if eq .Direction "in"}}tag-read{{else if eq .Direction "out"}}tag-bash{{else}}tag-edit{{end}}">{{.Direction}}</span>
              <span class="feed-from">{{.UserID}}</span>
              <span class="feed-text prose">{{.Text}}</span>
            </div>{{end}}
          </div>
          {{else}}
          <div style="padding:28px;text-align:center" class="ink-faint">No messages yet &mdash; ping <span class="mono" style="color:var(--phosphor)">@clawdy</span> on Telegram to see events flow here.</div>
          {{end}}
        </div>

        <!-- SERVER LOG TAIL -->
        <div class="sessions corner" id="logs" style="background:#070a0d">
          <div class="panel-head">
            <div>
              <span class="kicker">SERVER LOG · tail -f</span>
              <span class="ink-faint" style="margin-left:12px;font-size:11px">/var/log/openclaw.log</span>
            </div>
            <span class="chip">{{len .Logs}} lines</span>
          </div>
          {{if .Logs}}<pre class="logtail">{{range .Logs}}{{.}}
{{end}}</pre>{{else}}<div style="padding:28px;text-align:center" class="ink-faint">No logs yet.</div>{{end}}
        </div>

      </div>

      <!-- RIGHT RAIL -->
      <aside class="rail">
        <!-- Clawdy card -->
        <div class="rail-card corner">
          <div style="display:flex;align-items:flex-start;gap:14px">
            <div style="color:var(--phosphor);margin-top:2px">
              <svg width="44" height="44" viewBox="0 0 48 48" fill="none" style="filter:drop-shadow(0 0 6px rgba(120,255,170,0.6));display:block">
                <rect x="8" y="10" width="4" height="28" fill="currentColor"/>
                <rect x="18" y="14" width="4" height="24" fill="currentColor" opacity="0.85"/>
                <rect x="28" y="10" width="4" height="28" fill="currentColor"/>
                <rect x="8" y="10" width="24" height="3" fill="currentColor"/>
                <rect x="8" y="35" width="24" height="3" fill="currentColor"/>
                <rect x="37" y="20" width="4" height="4" fill="currentColor"><animate attributeName="opacity" values="1;0;1" dur="1s" repeatCount="indefinite"/></rect>
              </svg>
            </div>
            <div style="flex:1">
              <div class="eyebrow">CLAWDY · botd v1.3</div>
              <div class="prose" style="font-size:14px;font-weight:600;color:#f5faf4;margin-top:4px">
                I'm watching {{len .Sessions}} session{{if ne (len .Sessions) 1}}s{{end}}.
              </div>
              <div class="prose ink-dim" style="font-size:12.5px;line-height:1.5;margin-top:6px">
                {{.Bot}} · uptime {{.Uptime}}
              </div>
            </div>
          </div>
        </div>

        <!-- Operators -->
        <div class="rail-card" id="accounts">
          <div class="panel-head" style="padding:0 0 10px 0;border-bottom:1px solid var(--line)">
            <span class="kicker">OPERATORS</span>
            <span class="ink-faint" style="font-size:11px">{{len .Users}} total</span>
          </div>
          {{if .Users}}
          <div class="op-list">
            {{range .Users}}<div class="op-row">
              <div class="op-av">{{slice .Username 0 2}}</div>
              <div style="flex:1;min-width:0">
                <div style="font-size:13px;font-weight:500">{{.Username}}</div>
                <div class="ink-faint" style="font-size:11px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{.Email}}</div>
              </div>
              <span class="dot ok"></span>
            </div>{{end}}
          </div>
          {{else}}<div class="ink-faint" style="padding:12px 0;font-size:12px">No accounts provisioned.</div>{{end}}
        </div>

        <!-- Workspace -->
        <div class="rail-card">
          <div class="panel-head" style="padding:0 0 10px 0;border-bottom:1px solid var(--line)">
            <span class="kicker">WORKSPACE</span>
            <span class="ink-faint mono" style="font-size:11px">{{.Workspace}}</span>
          </div>
          {{if .Files}}
          <div style="margin-top:12px" class="ws-files">
            {{range .Files}}<div class="ws-file">
              <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6 4l-4 4 4 4M10 4l4 4-4 4"/></svg>
              <span class="mono" style="font-size:11.5px;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{.Path}}</span>
              <span class="ink-faint mono" style="font-size:11px">{{fmtSize .Size}}</span>
            </div>{{end}}
          </div>
          {{else}}<div class="ink-faint" style="padding:12px 0;font-size:12px">Workspace is empty.</div>{{end}}
        </div>

        <!-- Quick Actions -->
        <div class="rail-card">
          <div class="eyebrow" style="margin-bottom:12px">quick actions</div>
          <div class="qa-grid">
            {{if .HasGateway}}<a href="/gateway-launch" class="qa" style="text-decoration:none">
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M9 1L2 9h5l-1 6 7-8H8l1-6z"/></svg> open gateway
            </a>{{end}}
            <a href="/chat" class="qa" style="text-decoration:none">
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h12v10H2zM4 6l2 2-2 2M8 10h4"/></svg> web chat
            </a>
            <a href="/api/status" class="qa" style="text-decoration:none">
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 8h3l2-6 4 12 2-6h3"/></svg> api status
            </a>
            <a href="https://github.com/anchoo2kewl/openclaw" class="qa" style="text-decoration:none" target="_blank" rel="noreferrer">
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3a2 2 0 0 1 2-2h4v12H4a2 2 0 0 0-2 2V3zM14 3a2 2 0 0 0-2-2H8v12h4a2 2 0 0 1 2 2V3z"/></svg> source
            </a>
          </div>
        </div>
      </aside>
    </div>

    <div style="margin-top:40px;padding-top:16px;border-top:1px solid var(--line);display:flex;justify-content:space-between;font-size:11px;color:var(--ink-faint)">
      <span>refreshes every 15s · {{.Bot}}</span>
      <span><a href="https://github.com/anchoo2kewl/openclaw">github.com/anchoo2kewl/openclaw</a></span>
    </div>

  </div>
</div>
</body></html>`

const loginHTML = `<!doctype html>
<html lang=en><head>
<meta charset=utf-8>
<meta name=viewport content='width=device-width,initial-scale=1'>
<title>openclaw · sign in</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="apple-touch-icon" href="/favicon.svg">
<meta name="theme-color" content="#05070a">
<style>{{.CSS}}</style>
</head><body>
<div class="login-wrap">
  <div class="panel corner" style="width:420px;padding:36px 36px 32px">
    <div style="display:flex;align-items:center;gap:12px;margin-bottom:28px">
      <span style="width:24px;height:24px;display:grid;place-items:center;background:var(--phosphor);color:#041008;border-radius:3px;font-weight:900;font-size:14px">&#x276F;</span>
      <div>
        <div style="font-family:'JetBrains Mono',monospace;font-weight:700;font-size:13px;letter-spacing:0.1em">OPENCLAW</div>
        <div class="eyebrow" style="margin-top:2px">operator console</div>
      </div>
    </div>
    <div class="prose" style="font-family:'Inter Tight',sans-serif;font-size:22px;font-weight:600;color:#f5faf4;margin-bottom:6px;letter-spacing:-0.02em">
      Sign in
    </div>
    <div class="ink-dim" style="font-size:13px;margin-bottom:24px">
      to <span class="mono" style="color:var(--phosphor)">claw.biswas.me</span>
    </div>
    <form method="POST" action="/login">
      <label class="eyebrow" style="display:block;margin-bottom:6px">EMAIL OR USERNAME</label>
      <input name="identifier" type="text" autocomplete="username" autofocus required placeholder="admin" style="width:100%;margin-bottom:16px">
      <label class="eyebrow" style="display:block;margin-bottom:6px">PASSWORD</label>
      <input name="password" type="password" autocomplete="current-password" required placeholder="&#x2022;&#x2022;&#x2022;&#x2022;&#x2022;&#x2022;&#x2022;&#x2022;" style="width:100%;margin-bottom:24px">
      <button type="submit" class="btn btn-primary" style="width:100%;justify-content:center;padding:12px">
        Continue <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8h10M9 4l4 4-4 4"/></svg>
      </button>
      {{if .Error}}<div class="err-msg">Invalid credentials</div>{{end}}
    </form>
    <hr class="divider-dashed" style="margin:24px 0 16px">
    <div class="ink-faint" style="font-size:11px;text-align:center;line-height:1.6">
      by signing in you accept the MIT license.<br>
      no telemetry. your data stays on this vm.
    </div>
    <div style="margin-top:20px;display:flex;justify-content:space-between;font-size:12px">
      <a href="/">&#x2190; Back to home</a>
      <a href="https://github.com/anchoo2kewl/openclaw">GitHub</a>
    </div>
  </div>
</div>
</body></html>`

var (
	dashTmpl = template.Must(template.New("dash").Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string { return t.Format("15:04:05") },
		"fmtSize": fmtSize,
		"slice": func(s string, start, end int) string {
			if end > len(s) {
				end = len(s)
			}
			if start > len(s) {
				return ""
			}
			return s[start:end]
		},
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
	bot         *Bot
	jobRunner   *JobRunner
}

// DashboardConfig groups external wiring so main.go can plumb the gateway
// reverse proxy in without the caller of NewDashboard growing each time.
type DashboardConfig struct {
	Users        *UserStore
	GatewayURL   string // e.g. http://gateway:18789
	GatewayToken string // shared secret for gateway.auth.token
	Bot          *Bot   // for web chat to call Claude
}

// NewDashboard builds the full HTTP handler tree. Public endpoints: /,
// /login, /logout, /health. /api/status and /gateway/ require auth.
func NewDashboard(s *State, cfg DashboardConfig) http.Handler {
	var jr *JobRunner
	if cfg.Bot != nil {
		jr = NewJobRunner(cfg.Bot)
	}
	d := &dashboardServer{
		state:      s,
		users:      cfg.Users,
		sessions:   newSessionStore(12 * time.Hour),
		gatewayURL: cfg.GatewayURL,
		hasGateway: cfg.GatewayURL != "",
		bot:        cfg.Bot,
		jobRunner:  jr,
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

	mux.HandleFunc("/chat", d.handleChat)
	mux.HandleFunc("/api/chat", d.handleChatAPI)
	mux.HandleFunc("/api/history", d.handleHistoryAPI)
	mux.HandleFunc("/api/run", d.handleAPIRun)
	mux.HandleFunc("/api/orchestrate", d.handleOrchestrate)
	mux.HandleFunc("/api/webhook/github", d.handleGitHubWebhook)

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

// faviconSVG is the brand mark — a phosphor-green rounded square with a
// "❯" chevron, matching the Phosphor Ops design system. Same exact SVG
// used for /favicon.svg, the on-page brand-mark element across every
// template, AND the back-bar overlay injected into the gateway HTML, so
// the brand stays consistent on every surface.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <rect x="2" y="2" width="60" height="60" rx="8" fill="#3dff8a"/>
  <text x="32" y="42" text-anchor="middle" font-family="monospace" font-weight="900" font-size="36" fill="#041008">&#x276F;</text>
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

func (d *dashboardServer) handleHistoryAPI(w http.ResponseWriter, r *http.Request) {
	if d.sessions.authedEmail(r) == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if d.bot == nil || d.bot.history == nil {
		http.Error(w, `{"error":"history not available"}`, http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query().Get("q")
	w.Header().Set("Content-Type", "application/json")

	if query != "" {
		results := d.bot.history.SearchAll(query, 50)
		json.NewEncoder(w).Encode(results)
	} else {
		// Return recent history for all users.
		var all []HistoryEntry
		for _, uid := range d.state.Allowed {
			entries := d.bot.history.Recent(uid, 20)
			all = append(all, entries...)
		}
		json.NewEncoder(w).Encode(all)
	}
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
