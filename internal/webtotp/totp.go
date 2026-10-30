// Package webtotp is the optional second factor of the web panel
// (WEB-48): time-based one-time codes, RFC 6238 with SHA-1, 6 digits,
// 30-second steps, one step of drift either way. A Checker refuses a
// code it already took (and any older one) and allows MaxTries tries per
// TryWindow. The seed itself lives in the secret store (internal/secrets,
// name web.totp) of the UI process and never leaves it: the gateway asks
// the UI to check a code.
package webtotp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default, the one every authenticator app reads
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Step is the time step; Digits the code length.
const (
	Step   = 30 * time.Second
	Digits = 6
)

// Try limits.
const (
	MaxTries  = 5
	TryWindow = 5 * time.Minute
)

// Errors. They never hold a code or the seed.
var (
	ErrWrong   = errors.New("wrong code")
	ErrReplay  = errors.New("this code was used already; wait for the next one")
	ErrTooMany = errors.New("too many tries; wait a few minutes")
	ErrNoSeed  = errors.New("no sign-in code is set up")
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Code is the code of secret at t.
func Code(secret []byte, t time.Time) string {
	return codeAt(secret, uint64(t.Unix())/uint64(Step/time.Second))
}

func codeAt(secret []byte, counter uint64) string {
	mac := hmac.New(sha1.New, secret)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// Checker checks codes for one seed: replay and try limits.
type Checker struct {
	mu    sync.Mutex
	last  uint64 // the newest step taken; 0: none yet
	tries []time.Time
}

// Check takes code if it is right at now (±1 step), newer than the last
// one taken, and within the try limit.
func (c *Checker) Check(secret []byte, code string, now time.Time) error {
	if len(secret) == 0 {
		return ErrNoSeed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.tries[:0]
	for _, t := range c.tries {
		if now.Sub(t) < TryWindow {
			kept = append(kept, t)
		}
	}
	c.tries = kept
	if len(c.tries) >= MaxTries {
		return ErrTooMany
	}
	c.tries = append(c.tries, now)
	if len(code) != Digits || strings.Trim(code, "0123456789") != "" {
		return ErrWrong
	}
	cur := uint64(now.Unix()) / uint64(Step/time.Second)
	for _, step := range []uint64{cur - 1, cur, cur + 1} {
		if subtle.ConstantTimeCompare([]byte(codeAt(secret, step)), []byte(code)) == 1 {
			if step <= c.last {
				return ErrReplay
			}
			c.last = step
			c.tries = c.tries[:0]
			return nil
		}
	}
	return ErrWrong
}

// NewSecret makes a 160-bit seed and its base32 text (what an
// authenticator app takes).
func NewSecret() ([]byte, string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	return b, b32.EncodeToString(b), nil
}

// Decode reads a base32 seed (spaces and case ignored).
func Decode(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	s = strings.TrimRight(s, "=")
	return b32.DecodeString(s)
}

// URI is the otpauth:// link an authenticator app scans.
func URI(b32secret, account string) string {
	q := url.Values{}
	q.Set("secret", b32secret)
	q.Set("issuer", "termocode")
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/termocode:" + url.PathEscape(account) + "?" + q.Encode()
}
