package main

import (
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
	}
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
