package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookieName = "openclaw_session"

// sessionEntry is the payload we remember for each issued cookie token.
type sessionEntry struct {
	email  string
	expiry time.Time
}

// sessionStore is an in-memory, single-process session table. Sessions
// don't survive a restart — users just log in again. ttl is a sliding
// window extended on every successful lookup.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{
		sessions: make(map[string]sessionEntry),
		ttl:      ttl,
	}
}

func (s *sessionStore) issue(email string) string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.sessions[token] = sessionEntry{email: email, expiry: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return token
}

// lookup returns the email associated with token, or "" if the session is
// missing or expired. On success the expiry is extended (sliding window).
func (s *sessionStore) lookup(token string) string {
	if token == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[token]
	if !ok {
		return ""
	}
	if time.Now().After(e.expiry) {
		delete(s.sessions, token)
		return ""
	}
	e.expiry = time.Now().Add(s.ttl)
	s.sessions[token] = e
	return e.email
}

func (s *sessionStore) revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// authedEmail returns the logged-in email for the request, or "" if the
// caller has no valid session cookie.
func (s *sessionStore) authedEmail(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return s.lookup(c.Value)
}
