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
type User struct {
	Email string `json:"email"`
	Hash  string `json:"hash"`
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
	return json.Unmarshal(b, &u.users)
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
func (u *UserStore) Add(email, password string) error {
	email = normalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("invalid email")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	for i := range u.users {
		if u.users[i].Email == email {
			u.users[i].Hash = hash
			return u.save()
		}
	}
	u.users = append(u.users, User{Email: email, Hash: hash})
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

// List returns all emails in insertion order.
func (u *UserStore) List() []string {
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

// Verify returns the canonical email on success, or "" on any failure. The
// failure path still runs one full PBKDF2 hash against a dummy salt so that
// timing doesn't leak whether the email exists.
func (u *UserStore) Verify(email, password string) string {
	email = normalizeEmail(email)
	u.mu.RLock()
	defer u.mu.RUnlock()

	for i := range u.users {
		if u.users[i].Email == email {
			if verifyPassword(u.users[i].Hash, password) {
				return u.users[i].Email
			}
			return ""
		}
	}
	// Unknown email — burn the same amount of time on a dummy hash so an
	// attacker can't probe which emails are provisioned.
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
