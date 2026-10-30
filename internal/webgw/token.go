//go:build unix

package webgw

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Owner limits for a browser session.
const (
	// DefaultIdle ends a session nobody used for this long.
	DefaultIdle = 15 * time.Minute
	// DefaultMax ends every session this long after it began.
	DefaultMax = 12 * time.Hour
)

// Role is what a session may do. v1 has only control.
type Role string

// Roles. v1 issues only RoleControl; RoleViewer (v2) may read only.
const (
	RoleControl Role = "control"
	RoleViewer  Role = "viewer"
)

// ErrTokenUnknown: no live session has this token. Errors never hold the
// token.
var ErrTokenUnknown = errors.New("unknown session token")

// ErrTokenExpired: the session ran past its idle or max time; it is
// forgotten, so the next check is ErrTokenUnknown.
var ErrTokenExpired = errors.New("the session token expired")

// ErrTooManySessions: MaxSessions tokens are live already.
var ErrTooManySessions = errors.New("too many web sessions")

// MaxSessions caps the live tokens (one per browser tab).
const MaxSessions = 16

// Len is the number of tokens kept (expired ones not swept yet included).
func (t *Tokens) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.byHash)
}

// RevokeAll drops every token (the kill switch) and says how many.
func (t *Tokens) RevokeAll() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(t.byHash)
	clear(t.byHash)
	return n
}

// sweepLocked drops the expired sessions. t.mu is held.
func (t *Tokens) sweepLocked(now time.Time) {
	for h, s := range t.byHash {
		if t.expired(s, now) {
			delete(t.byHash, h)
		}
	}
}

// Sess is one browser session. It holds no secret: the token itself is
// known only to the browser (sessionStorage) and as a digest here.
type Sess struct {
	// ID names the session in the audit log and the TUI; it is random and
	// not derived from the token.
	ID       string
	Device   string
	Role     Role
	Created  time.Time
	LastSeen time.Time
}

// Tokens are the live session tokens, kept by SHA-256 digest. A lookup by
// digest leaks nothing useful through timing: an attacker cannot steer
// the digest of a guess toward a real one.
type Tokens struct {
	idle, max time.Duration

	mu     sync.Mutex
	byHash map[[sha256.Size]byte]*Sess
}

// NewTokens keeps sessions that end after idle without use or max after
// they began.
func NewTokens(idle, max time.Duration) *Tokens {
	return &Tokens{idle: idle, max: max, byHash: map[[sha256.Size]byte]*Sess{}}
}

// Issue makes a new session and returns its token (once; it is not kept)
// and a copy of the session. With MaxSessions live it first sweeps the
// expired ones, then refuses with ErrTooManySessions.
func (t *Tokens) Issue(device string, role Role, now time.Time) (string, Sess, error) {
	tok, err := randomToken()
	if err != nil {
		return "", Sess{}, err
	}
	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return "", Sess{}, err
	}
	s := &Sess{ID: "s-" + hex.EncodeToString(id), Device: device, Role: role, Created: now, LastSeen: now}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.byHash) >= MaxSessions {
		t.sweepLocked(now)
		if len(t.byHash) >= MaxSessions {
			return "", Sess{}, ErrTooManySessions
		}
	}
	t.byHash[sha256.Sum256([]byte(tok))] = s
	return tok, *s, nil
}

// Check returns the session of tok and marks it used at now. An expired
// session is dropped and reported once as ErrTokenExpired.
func (t *Tokens) Check(tok string, now time.Time) (Sess, error) {
	h := sha256.Sum256([]byte(tok))
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.byHash[h]
	if !ok {
		return Sess{}, ErrTokenUnknown
	}
	if t.expired(s, now) {
		delete(t.byHash, h)
		return Sess{}, ErrTokenExpired
	}
	s.LastSeen = now
	return *s, nil
}

// expired: the session was idle for t.idle or longer, or began t.max or
// longer ago.
func (t *Tokens) expired(s *Sess, now time.Time) bool {
	return !now.Before(s.LastSeen.Add(t.idle)) || !now.Before(s.Created.Add(t.max))
}
