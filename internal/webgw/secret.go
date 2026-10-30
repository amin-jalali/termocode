//go:build unix

package webgw

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// LaunchSecretTTL is how long a launch secret can be redeemed.
const LaunchSecretTTL = 60 * time.Second

// secretBytes is the size of a launch secret and of a session token:
// 256 random bits, so guessing is hopeless (T13).
const secretBytes = 32

// Redeem errors. They name the rule, never a value. The HTTP answer is the
// same for all of them; the difference is for the audit log.
var (
	ErrSecretWrong   = errors.New("wrong launch secret")
	ErrSecretUsed    = errors.New("the launch secret was already used")
	ErrSecretExpired = errors.New("the launch secret expired")
)

// LaunchSecret is the one-time secret that lets the first browser tab in
// (it travels in the URL #fragment, never in a query, argv or a log). Only
// its SHA-256 digest is kept.
type LaunchSecret struct {
	mu   sync.Mutex
	hash [sha256.Size]byte
	exp  time.Time
	used bool
	bad  int // wrong tries so far
}

// NewLaunchSecret makes a fresh secret that expires LaunchSecretTTL after
// now. plain goes to the browser once; the gateway keeps only s.
func NewLaunchSecret(now time.Time) (plain string, s *LaunchSecret, err error) {
	plain, err = randomToken()
	if err != nil {
		return "", nil, err
	}
	return plain, &LaunchSecret{hash: sha256.Sum256([]byte(plain)), exp: now.Add(LaunchSecretTTL)}, nil
}

// Redeem uses the secret up if plain is right, unused and not expired at
// now. The check compares SHA-256 digests in constant time, so the time it
// takes says nothing about how much of a guess was right.
func (s *LaunchSecret) Redeem(plain string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used {
		return ErrSecretUsed
	}
	if !now.Before(s.exp) {
		return ErrSecretExpired
	}
	sum := sha256.Sum256([]byte(plain))
	if subtle.ConstantTimeCompare(sum[:], s.hash[:]) != 1 {
		s.bad++
		return ErrSecretWrong
	}
	s.used = true
	return nil
}

// String never shows the digest or anything else secret.
func (s *LaunchSecret) String() string { return "LaunchSecret{…}" }

// randomToken is secretBytes from crypto/rand as unpadded base64url
// (43 characters, safe in a URL fragment and in JSON).
func randomToken() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
