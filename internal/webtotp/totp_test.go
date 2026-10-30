package webtotp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var rfcSecret = []byte("12345678901234567890")

// RFC 6238 appendix B (SHA-1), cut to 6 digits.
func TestRFC6238Vectors(t *testing.T) {
	for ts, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := Code(rfcSecret, time.Unix(ts, 0)); got != want {
			t.Errorf("T=%d: %s, want %s", ts, got, want)
		}
	}
}

// One step either side counts (clock drift); two steps do not.
func TestCheckWindow(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, d := range []time.Duration{-Step, 0, Step} {
		c := &Checker{}
		if err := c.Check(rfcSecret, Code(rfcSecret, now.Add(d)), now); err != nil {
			t.Errorf("drift %v: %v", d, err)
		}
	}
	c := &Checker{}
	if err := c.Check(rfcSecret, Code(rfcSecret, now.Add(2*Step)), now); !errors.Is(err, ErrWrong) {
		t.Errorf("two steps ahead: %v", err)
	}
}

// Replay (WEB-48): a code that was used once is refused, and so is any
// older one, until a newer step comes.
func TestCheckReplay(t *testing.T) {
	now := time.Unix(1700000000, 0)
	c := &Checker{}
	code := Code(rfcSecret, now)
	if err := c.Check(rfcSecret, code, now); err != nil {
		t.Fatal(err)
	}
	if err := c.Check(rfcSecret, code, now.Add(time.Second)); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: %v", err)
	}
	if err := c.Check(rfcSecret, Code(rfcSecret, now.Add(-Step)), now); !errors.Is(err, ErrReplay) {
		t.Fatalf("older code: %v", err)
	}
	later := now.Add(Step)
	if err := c.Check(rfcSecret, Code(rfcSecret, later), later); err != nil {
		t.Fatalf("next step: %v", err)
	}
}

// Rate limit (WEB-48): 5 tries per 5 minutes, right or wrong; then even
// the right code waits.
func TestCheckRateLimit(t *testing.T) {
	now := time.Unix(1700000000, 0)
	c := &Checker{}
	for i := 0; i < MaxTries; i++ {
		_ = c.Check(rfcSecret, "000000", now)
	}
	if err := c.Check(rfcSecret, Code(rfcSecret, now), now); !errors.Is(err, ErrTooMany) {
		t.Fatalf("sixth try: %v", err)
	}
	later := now.Add(TryWindow)
	if err := c.Check(rfcSecret, Code(rfcSecret, later), later); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestBadCodes(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, code := range []string{"", "12345", "1234567", "12a456", " 12345"} {
		c := &Checker{}
		if err := c.Check(rfcSecret, code, now); !errors.Is(err, ErrWrong) {
			t.Errorf("%q: %v", code, err)
		}
	}
	if err := (&Checker{}).Check(nil, "123456", now); err == nil {
		t.Error("an empty secret checked")
	}
}

func TestNewSecretAndURI(t *testing.T) {
	sec, b32, err := NewSecret()
	if err != nil || len(sec) != 20 || len(b32) != 32 || strings.ContainsAny(b32, "=") {
		t.Fatalf("%d bytes, %q, %v", len(sec), b32, err)
	}
	back, err := Decode(b32)
	if err != nil || string(back) != string(sec) {
		t.Fatal("base32 does not round-trip")
	}
	u := URI(b32, "box.tail1234.ts.net")
	if !strings.HasPrefix(u, "otpauth://totp/termocode:box.tail1234.ts.net?") || !strings.Contains(u, "secret="+b32) ||
		!strings.Contains(u, "issuer=termocode") || !strings.Contains(u, "period=30") || !strings.Contains(u, "digits=6") {
		t.Fatalf("uri %q", u)
	}
}
