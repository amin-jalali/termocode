//go:build unix

package webgw

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func newSecret(t *testing.T) (string, *LaunchSecret) {
	t.Helper()
	plain, s, err := NewLaunchSecret(t0)
	if err != nil {
		t.Fatal(err)
	}
	return plain, s
}

// TestLaunchSecretFormat: 32 random bytes as unpadded base64url (43
// characters), different every time.
func TestLaunchSecretFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		plain, _ := newSecret(t)
		b, err := base64.RawURLEncoding.DecodeString(plain)
		if err != nil || len(b) != 32 || len(plain) != 43 {
			t.Fatalf("secret %d chars, %d bytes, %v", len(plain), len(b), err)
		}
		if seen[plain] {
			t.Fatal("secret repeated")
		}
		seen[plain] = true
	}
}

// TestLaunchSecretSingleUse: the right secret works once (T11).
func TestLaunchSecretSingleUse(t *testing.T) {
	plain, s := newSecret(t)
	if err := s.Redeem(plain, t0.Add(time.Second)); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if err := s.Redeem(plain, t0.Add(2*time.Second)); !errors.Is(err, ErrSecretUsed) {
		t.Fatalf("second redeem: %v, want ErrSecretUsed", err)
	}
}

// TestLaunchSecretExpires: valid for LaunchSecretTTL (5 minutes: time to
// paste the link by hand when no browser opens), then never.
func TestLaunchSecretExpires(t *testing.T) {
	if LaunchSecretTTL != 5*time.Minute {
		t.Fatalf("TTL %v, want 5m", LaunchSecretTTL)
	}
	plain, s := newSecret(t)
	if err := s.Redeem(plain, t0.Add(LaunchSecretTTL)); !errors.Is(err, ErrSecretExpired) {
		t.Fatalf("at the TTL: %v, want ErrSecretExpired", err)
	}
	plain, s = newSecret(t)
	if err := s.Redeem(plain, t0.Add(LaunchSecretTTL-time.Nanosecond)); err != nil {
		t.Fatalf("just before the TTL: %v", err)
	}
}

func TestLaunchSecretWrongRefused(t *testing.T) {
	plain, s := newSecret(t)
	for _, guess := range []string{"", "x", plain[:42], plain + "A", strings.ToUpper(plain),
		plain[1:] + plain[:1], strings.Repeat("A", 43), strings.Repeat("A", 5000)} {
		if guess == plain {
			continue
		}
		if err := s.Redeem(guess, t0); !errors.Is(err, ErrSecretWrong) {
			t.Fatalf("guess %.20q: %v, want ErrSecretWrong", guess, err)
		}
		// Reset the strike count so this table never burns the secret.
		s.bad = 0
	}
	if err := s.Redeem(plain, t0); err != nil {
		t.Fatalf("right secret after wrong guesses: %v", err)
	}
}

// TestLaunchSecretBurnsAfter5: the fifth wrong try burns the secret
// (ErrSecretBurned, once, for the audit log); after that even the right
// secret is refused with ErrSecretClosed (T13).
func TestLaunchSecretBurnsAfter5(t *testing.T) {
	if MaxBadTries != 5 {
		t.Fatalf("MaxBadTries = %d, want 5", MaxBadTries)
	}
	plain, s := newSecret(t)
	for i := 1; i < MaxBadTries; i++ {
		if err := s.Redeem("wrong", t0); !errors.Is(err, ErrSecretWrong) {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if err := s.Redeem("wrong", t0); !errors.Is(err, ErrSecretBurned) {
		t.Fatalf("try %d: %v, want ErrSecretBurned", MaxBadTries, err)
	}
	for _, guess := range []string{"wrong", plain} {
		if err := s.Redeem(guess, t0); !errors.Is(err, ErrSecretClosed) {
			t.Fatalf("after burn, %v, want ErrSecretClosed", err)
		}
	}
}

// TestLaunchSecretFourWrongThenRight: fewer than five wrong tries do not
// lock the owner out.
func TestLaunchSecretFourWrongThenRight(t *testing.T) {
	plain, s := newSecret(t)
	for i := 1; i < MaxBadTries; i++ {
		_ = s.Redeem("wrong", t0)
	}
	if err := s.Redeem(plain, t0); err != nil {
		t.Fatalf("right secret after %d wrong: %v", MaxBadTries-1, err)
	}
}

// TestLaunchSecretRedeemRace: many tabs racing with the right secret; one
// wins, the rest see ErrSecretUsed.
func TestLaunchSecretRedeemRace(t *testing.T) {
	plain, s := newSecret(t)
	const n = 32
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Redeem(plain, t0)
		}()
	}
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrSecretUsed) {
			t.Fatalf("loser: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners, want 1", wins)
	}
}

// TestLaunchSecretErrorsHoldNoValue: an error names the rule, never the
// value tried or the real one (T8).
func TestLaunchSecretErrorsHoldNoValue(t *testing.T) {
	plain, s := newSecret(t)
	guess := "guess-" + "value-123"
	for _, err := range []error{s.Redeem(guess, t0), s.Redeem(plain, t0), s.Redeem(plain, t0)} {
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), guess) || strings.Contains(err.Error(), plain) {
			t.Fatalf("error holds a value: %v", err)
		}
	}
}

// TestRedeemConstantTime: the secret and token checks compare SHA-256
// digests with crypto/subtle, never with == or bytes.Equal (T11, T13).
func TestRedeemConstantTime(t *testing.T) {
	src, err := os.ReadFile("secret.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "subtle.ConstantTimeCompare(") {
		t.Error("secret.go does not use subtle.ConstantTimeCompare")
	}
	for _, bad := range []string{"bytes.Equal(", "== s.hash", "s.hash ==", "== plain", "plain =="} {
		if strings.Contains(s, bad) {
			t.Errorf("secret.go compares with %q", bad)
		}
	}
}

// TestLaunchSecretKeepsNoPlainText: only the digest is kept.
func TestLaunchSecretKeepsNoPlainText(t *testing.T) {
	plain, s := newSecret(t)
	if strings.Contains(strings.ToLower(s.String()), strings.ToLower(plain)) {
		t.Fatal("String() shows the secret")
	}
}
