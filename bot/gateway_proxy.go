package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// newGatewayProxy returns an http.Handler that reverse-proxies every
// request under /gateway/ to the sibling openclaw-gateway container,
// after confirming the caller has a valid dashboard session cookie.
//
// The proxy:
//   - strips the leading "/gateway" prefix before forwarding
//   - injects the shared-secret Authorization header so the gateway's
//     token auth accepts us
//   - transparently supports WebSocket upgrades (httputil.ReverseProxy
//     has handled them natively since Go 1.12)
//
// If target is empty or cannot be parsed, a 503 handler is returned
// instead — the rest of the dashboard keeps working.
func newGatewayProxy(target, token string, sessions *sessionStore) http.Handler {
	if target == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "gateway not configured", http.StatusServiceUnavailable)
		})
	}
	u, err := url.Parse(target)
	if err != nil {
		log.Error().Err(err).Str("target", target).Msg("invalid GATEWAY_URL")
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "gateway misconfigured", http.StatusServiceUnavailable)
		})
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = u.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/gateway")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		// Don't leak our own cookie jar upstream.
		req.Header.Del("Cookie")
		// Tell upstream we can't handle gzip so we can easily rewrite
		// text/html responses on the way back.
		req.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = injectBackBar
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Warn().Err(err).Str("path", r.URL.Path).Msg("gateway proxy error")
		http.Error(w, "gateway unavailable: "+err.Error(), http.StatusBadGateway)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessions.authedEmail(r) == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// backBarHTML is injected into every text/html response flowing through
// the gateway reverse proxy so logged-in operators always have a visible
// way back to the openclaw dashboard from inside the upstream Control UI.
// The overlay is fixed-position + high z-index so it sits above the
// gateway app regardless of the internal DOM layout. The brand mark is
// the same SVG used by /favicon.svg and the on-page logo so all surfaces
// share one glyph.
const backBarHTML = `<style>
#openclaw-backbar{position:fixed;top:0;left:0;right:0;z-index:2147483647;background:rgba(11,13,16,.94);backdrop-filter:saturate(140%) blur(10px);-webkit-backdrop-filter:saturate(140%) blur(10px);border-bottom:1px solid #1f2632;color:#e6e9ef;font:500 13px/1 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;padding:9px 16px;display:flex;align-items:center;gap:14px;height:44px;box-sizing:border-box}
#openclaw-backbar .ocb-brand{display:inline-flex;align-items:center;gap:8px;font-weight:700;color:#e6e9ef}
#openclaw-backbar .ocb-brand svg{width:22px;height:22px;display:block;filter:drop-shadow(0 4px 14px rgba(99,102,241,0.35))}
#openclaw-backbar a{color:#e6e9ef;text-decoration:none;display:inline-flex;align-items:center;gap:6px;padding:6px 10px;border-radius:6px;border:1px solid #2a3444;background:#141820;transition:background .12s ease}
#openclaw-backbar a:hover{background:#1f2632}
#openclaw-backbar .tag{color:#8b94a8;font-weight:400;padding-left:2px}
#openclaw-backbar .spacer{flex:1}
body{padding-top:44px !important}
</style>
<div id="openclaw-backbar">
  <a href="/" title="Back to openclaw dashboard">← Dashboard</a>
  <div class="ocb-brand">
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><defs><linearGradient id="gocb" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#6366f1"/><stop offset="1" stop-color="#8b5cf6"/></linearGradient></defs><rect x="2" y="2" width="60" height="60" rx="14" fill="url(#gocb)"/><path d="M20 22 L32 14 L44 22 L44 42 L32 50 L20 42 Z" fill="none" stroke="#fff" stroke-width="3.5" stroke-linejoin="round"/><circle cx="32" cy="32" r="4.5" fill="#fff"/></svg>
    <span>openclaw</span><span class=tag>· gateway</span>
  </div>
  <div class=spacer></div>
  <a href="/logout-nav" onclick="event.preventDefault();fetch('/logout',{method:'POST',credentials:'same-origin'}).then(()=>location.href='/')">Log out</a>
</div>
`

// injectBackBar is a httputil.ReverseProxy.ModifyResponse hook. If the
// response is HTML, rewrite the body to include backBarHTML right after
// <body ...>. Anything else (JS, CSS, JSON, images) passes through
// untouched.
func injectBackBar(resp *http.Response) error {
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		return nil
	}
	// Upstream may still emit a compressed body even though we asked for
	// identity — decode if necessary.
	var bodyReader io.ReadCloser = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		bodyReader = gr
	}
	body, err := io.ReadAll(bodyReader)
	_ = bodyReader.Close()
	if err != nil {
		return err
	}
	idx := bytes.Index(body, []byte("<body"))
	if idx == -1 {
		// Not a full HTML document (probably a fragment) — leave alone.
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", itoa(len(body)))
		return nil
	}
	// Find the closing '>' of the <body ...> tag.
	close := bytes.IndexByte(body[idx:], '>')
	if close == -1 {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	insertAt := idx + close + 1

	var out bytes.Buffer
	out.Grow(len(body) + len(backBarHTML))
	out.Write(body[:insertAt])
	out.WriteString(backBarHTML)
	out.Write(body[insertAt:])

	resp.Body = io.NopCloser(&out)
	resp.Header.Del("Content-Encoding")
	resp.ContentLength = int64(out.Len())
	resp.Header.Set("Content-Length", itoa(out.Len()))
	return nil
}

func itoa(n int) string {
	// Tiny wrapper so we don't have to import strconv just for this
	// one-call site in this file (strconv is already imported by main).
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
