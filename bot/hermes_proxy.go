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

// newHermesProxy returns an http.Handler that reverse-proxies every
// request under /hermes/ to the hermes-agent container, after
// confirming the caller has a valid dashboard session cookie.
//
// The proxy:
//   - strips the leading "/hermes" prefix before forwarding
//   - does NOT inject an Authorization header (hermes manages its
//     own session tokens internally; irrelevant when proxied)
//   - transparently supports WebSocket upgrades
//   - injects a navigation back-bar and <base href="/hermes/"> into
//     HTML responses so the SPA's relative paths resolve correctly
//
// If target is empty or cannot be parsed, a 503 handler is returned.
func newHermesProxy(target string, sessions *sessionStore) http.Handler {
	if target == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		})
	}
	u, err := url.Parse(target)
	if err != nil {
		log.Error().Err(err).Str("target", target).Msg("invalid HERMES_URL")
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "hermes misconfigured", http.StatusServiceUnavailable)
		})
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = u.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/hermes")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		// Don't leak our own cookie jar upstream.
		req.Header.Del("Cookie")
		// Tell upstream we can't handle gzip so we can rewrite HTML.
		req.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = injectHermesBar
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Warn().Err(err).Str("path", r.URL.Path).Msg("hermes proxy error")
		http.Error(w, "hermes unavailable: "+err.Error(), http.StatusBadGateway)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessions.authedEmail(r) == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

const hermesBackBarHTML = `<style>
@import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&display=swap');
#openclaw-backbar{position:fixed;top:0;left:0;right:0;z-index:2147483647;background:rgba(5,7,10,0.88);backdrop-filter:blur(16px);-webkit-backdrop-filter:blur(16px);border-bottom:1px solid rgba(120,255,170,0.08);color:#dbe6d9;font:500 12px/1 'JetBrains Mono',ui-monospace,monospace;padding:0 16px;display:flex;align-items:center;gap:14px;height:44px;box-sizing:border-box;letter-spacing:0.06em;text-transform:uppercase}
#openclaw-backbar a{color:#dbe6d9;text-decoration:none;display:inline-flex;align-items:center;gap:6px;padding:6px 10px;border-radius:3px;border:1px solid rgba(120,255,170,0.16);background:transparent;transition:all .12s ease;font-size:11px}
#openclaw-backbar a:hover{color:#3dff8a;border-color:#3dff8a;background:rgba(120,255,170,0.08)}
#openclaw-backbar .ocb-brand{display:inline-flex;align-items:center;gap:8px;font-weight:700;color:#dbe6d9;font-size:13px;letter-spacing:0.1em}
#openclaw-backbar .ocb-mark{width:20px;height:20px;display:grid;place-items:center;background:#3dff8a;color:#041008;border-radius:3px;font-weight:900;font-size:12px;box-shadow:0 0 12px -2px #3dff8a}
#openclaw-backbar .tag{color:#5a6a62;font-weight:400;padding-left:2px;font-size:11px}
#openclaw-backbar .spacer{flex:1}
body{padding-top:44px !important}
</style>
<div id="openclaw-backbar">
  <a href="/" title="Back to openclaw dashboard">&#x2190; Dashboard</a>
  <div class="ocb-brand">
    <span class="ocb-mark">&#x276F;</span>
    <span>OPENCLAW</span><span class="tag">&#xB7; hermes</span>
  </div>
  <div class="spacer"></div>
  <a href="/logout-nav" onclick="event.preventDefault();fetch('/logout',{method:'POST',credentials:'same-origin'}).then(()=>location.href='/')">Log out</a>
</div>
`

// hermesBaseTag is injected into <head> so the React SPA's relative
// asset and API paths resolve correctly under the /hermes/ prefix.
const hermesBaseTag = `<base href="/hermes/">`

func injectHermesBar(resp *http.Response) error {
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		return nil
	}
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

	// Inject <base href="/hermes/"> into <head>.
	headIdx := bytes.Index(body, []byte("<head"))
	if headIdx != -1 {
		closeHead := bytes.IndexByte(body[headIdx:], '>')
		if closeHead != -1 {
			insertHead := headIdx + closeHead + 1
			var tmp bytes.Buffer
			tmp.Grow(len(body) + len(hermesBaseTag))
			tmp.Write(body[:insertHead])
			tmp.WriteString(hermesBaseTag)
			tmp.Write(body[insertHead:])
			body = tmp.Bytes()
		}
	}

	// Inject back-bar after <body ...>.
	bodyIdx := bytes.Index(body, []byte("<body"))
	if bodyIdx == -1 {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", itoa(len(body)))
		return nil
	}
	closeBody := bytes.IndexByte(body[bodyIdx:], '>')
	if closeBody == -1 {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	insertAt := bodyIdx + closeBody + 1

	var out bytes.Buffer
	out.Grow(len(body) + len(hermesBackBarHTML))
	out.Write(body[:insertAt])
	out.WriteString(hermesBackBarHTML)
	out.Write(body[insertAt:])

	resp.Body = io.NopCloser(&out)
	resp.Header.Del("Content-Encoding")
	resp.ContentLength = int64(out.Len())
	resp.Header.Set("Content-Length", itoa(out.Len()))
	return nil
}
