package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "openclaw_session"

// sessionStore is an in-memory, single-process session table. openclaw is a
// single-user tool, so we don't bother persisting sessions across restarts —
// you just log in again. Sessions expire after ttl of inactivity.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	ttl      time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{
		sessions: make(map[string]time.Time),
		ttl:      ttl,
	}
}

func (s *sessionStore) issue() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read failing is catastrophic; log and return an unusable token
		// rather than panic inside a request handler.
		return ""
	}
	token := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return token
}

// valid checks the token and extends its expiry on success (sliding window).
func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		return false
	}
	s.sessions[token] = time.Now().Add(s.ttl)
	return true
}

func (s *sessionStore) revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// authed returns true if the request carries a valid session cookie.
func (s *sessionStore) authed(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	return s.valid(c.Value)
}

// checkPassword is a constant-time comparison so probing the login form
// doesn't leak the password length via timing.
func checkPassword(expected, got string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}
