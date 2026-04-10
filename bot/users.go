package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// User is one account authorized to log in to the management dashboard.
// Password is stored as a single encoded string produced by hashPassword.
//
// Username is an optional short login alias. When omitted on useradd it
// defaults to the local part of the email (everything before the @).
// Stored lowercased. Both Email and Username are unique identifiers for
// the same account and Verify accepts either.
type User struct {
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	Hash     string `json:"hash"`
}

// usernameFromEmail returns the lowercased part before the @ — good enough
// for the "admin@openclaw.local" → "admin" alias.
func usernameFromEmail(email string) string {
	email = normalizeEmail(email)
	if i := strings.Index(email, "@"); i > 0 {
		return email[:i]
	}
	return email
}

// UserStore is a tiny append/replace-in-place JSON-file-backed user table.
// The whole file is rewritten atomically on every mutation; this scales to
// hundreds of users before we'd want anything more sophisticated.
type UserStore struct {
	path  string
	mu    sync.RWMutex
	users []User
}

func NewUserStore(path string) (*UserStore, error) {
	us := &UserStore{path: path}
	if err := us.load(); err != nil {
		return nil, err
	}
	return us, nil
}

func (u *UserStore) load() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	b, err := os.ReadFile(u.path)
	if errors.Is(err, os.ErrNotExist) {
		u.users = nil
		return nil
	}
	if err != nil {
		return err
	}
	b = trimSpace(b)
	if len(b) == 0 {
		u.users = nil
		return nil
	}
	if err := json.Unmarshal(b, &u.users); err != nil {
		return err
	}
	// Back-fill Username from Email's local part for records written
	// before the schema change. Normalize to lowercase.
	dirty := false
	for i := range u.users {
		u.users[i].Email = normalizeEmail(u.users[i].Email)
		if u.users[i].Username == "" {
			u.users[i].Username = usernameFromEmail(u.users[i].Email)
			dirty = true
		} else {
			u.users[i].Username = strings.ToLower(strings.TrimSpace(u.users[i].Username))
		}
	}
	if dirty {
		return u.save()
	}
	return nil
}

func (u *UserStore) save() error {
	if err := os.MkdirAll(filepath.Dir(u.path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(u.users, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := u.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, u.path)
}

func normalizeEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}

// Add upserts a user. If the email already exists, its hash is replaced.
// username, if empty, is derived from the email local part.
func (u *UserStore) Add(email, username, password string) error {
	email = normalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("invalid email")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if username == "" {
		username = usernameFromEmail(email)
	} else {
		username = strings.ToLower(strings.TrimSpace(username))
	}
	if strings.Contains(username, "@") || strings.ContainsAny(username, " \t\r\n") {
		return errors.New("username must not contain whitespace or @")
	}

	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	// Refuse to shadow another account's username.
	for i := range u.users {
		if u.users[i].Email != email && u.users[i].Username == username {
			return fmt.Errorf("username %q already taken by %s", username, u.users[i].Email)
		}
	}
	for i := range u.users {
		if u.users[i].Email == email {
			u.users[i].Username = username
			u.users[i].Hash = hash
			return u.save()
		}
	}
	u.users = append(u.users, User{Email: email, Username: username, Hash: hash})
	return u.save()
}

func (u *UserStore) Delete(email string) error {
	email = normalizeEmail(email)
	u.mu.Lock()
	defer u.mu.Unlock()
	for i := range u.users {
		if u.users[i].Email == email {
			u.users = append(u.users[:i], u.users[i+1:]...)
			return u.save()
		}
	}
	return errors.New("user not found")
}

// UserRow is the public-facing summary of a provisioned account, used
// by the dashboard "accounts" table.
type UserRow struct {
	Email    string
	Username string
}

// List returns all accounts in insertion order.
func (u *UserStore) List() []UserRow {
	u.mu.RLock()
	defer u.mu.RUnlock()
	out := make([]UserRow, len(u.users))
	for i, user := range u.users {
		out[i] = UserRow{Email: user.Email, Username: user.Username}
	}
	return out
}

// Emails is a convenience accessor that returns just the email column,
// still used by the `openclaw userlist` subcommand.
func (u *UserStore) Emails() []string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	out := make([]string, len(u.users))
	for i, user := range u.users {
		out[i] = user.Email
	}
	return out
}

func (u *UserStore) Empty() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.users) == 0
}

// Verify accepts either an email or a username as the identifier. Returns
// the canonical email on success, or "" on any failure. The failure path
// still runs one full PBKDF2 hash so timing can't reveal whether the
// identifier exists.
func (u *UserStore) Verify(identifier, password string) string {
	ident := strings.ToLower(strings.TrimSpace(identifier))
	u.mu.RLock()
	defer u.mu.RUnlock()

	for i := range u.users {
		if u.users[i].Email == ident || u.users[i].Username == ident {
			if verifyPassword(u.users[i].Hash, password) {
				return u.users[i].Email
			}
			return ""
		}
	}
	// Unknown identifier — burn the same amount of time on a dummy hash
	// so an attacker can't probe which accounts are provisioned.
	_ = verifyPassword(dummyHash, password)
	return ""
}

// --------------------------- password hashing -----------------------------

const (
	pbkdf2Iters = 600_000 // OWASP 2023 recommendation for PBKDF2-SHA256
	saltLen     = 16
	keyLen      = 32
)

// dummyHash is a real PBKDF2 hash of a random unknown password. It only
// exists to keep the "no such user" login path similar in wall-time to the
// real verification path.
const dummyHash = "pbkdf2-sha256$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iters, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iters,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(want, got) == 1
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}
