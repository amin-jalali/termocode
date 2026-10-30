//go:build unix

package webgw

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenIssueAndCheck(t *testing.T) {
	ts := NewTokens(DefaultIdle, DefaultMax)
	tok, sess, err := ts.Issue("loc-3f9a", RoleControl, t0)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := base64.RawURLEncoding.DecodeString(tok); err != nil || len(b) != 32 {
		t.Fatalf("token is not 32 random bytes: %v", err)
	}
	if sess.Device != "loc-3f9a" || sess.Role != RoleControl || !sess.Created.Equal(t0) || sess.ID == "" {
		t.Fatalf("session %+v", sess)
	}
	if strings.Contains(sess.ID, tok) || strings.Contains(tok, sess.ID) {
		t.Fatal("the session id is part of the token")
	}
	got, err := ts.Check(tok, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sess.ID || !got.LastSeen.Equal(t0.Add(time.Minute)) {
		t.Fatalf("checked session %+v", got)
	}
	tok2, sess2, err := ts.Issue("loc-3f9a", RoleControl, t0)
	if err != nil || tok2 == tok || sess2.ID == sess.ID {
		t.Fatalf("second token not distinct: %v", err)
	}
}

func TestTokenUnknownRejected(t *testing.T) {
	ts := NewTokens(DefaultIdle, DefaultMax)
	tok, _, err := ts.Issue("d", RoleControl, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "x", tok[:42], tok + "A", strings.ToUpper(tok), " " + tok} {
		if bad == tok {
			continue
		}
		if _, err := ts.Check(bad, t0); !errors.Is(err, ErrTokenUnknown) {
			t.Errorf("token %.12q: %v, want ErrTokenUnknown", bad, err)
		}
	}
}

// TestTokenIdleExpiry: a token unused for DefaultIdle (15 min) is gone for
// good; each use moves the idle deadline (T12).
func TestTokenIdleExpiry(t *testing.T) {
	if DefaultIdle != 15*time.Minute {
		t.Fatalf("DefaultIdle %v", DefaultIdle)
	}
	ts := NewTokens(DefaultIdle, DefaultMax)
	tok, _, err := ts.Issue("d", RoleControl, t0)
	if err != nil {
		t.Fatal(err)
	}
	now := t0
	for i := 0; i < 3; i++ { // used every 14 min: stays alive past 15 min
		now = now.Add(DefaultIdle - time.Minute)
		if _, err := ts.Check(tok, now); err != nil {
			t.Fatalf("use %d: %v", i, err)
		}
	}
	now = now.Add(DefaultIdle)
	if _, err := ts.Check(tok, now); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("after idle: %v, want ErrTokenExpired", err)
	}
	if _, err := ts.Check(tok, now); !errors.Is(err, ErrTokenUnknown) {
		t.Fatalf("after expiry the token must be forgotten: %v", err)
	}
}

// TestTokenMaxLifetime: even a token in steady use ends DefaultMax (12 h)
// after it was issued (T12).
func TestTokenMaxLifetime(t *testing.T) {
	if DefaultMax != 12*time.Hour {
		t.Fatalf("DefaultMax %v", DefaultMax)
	}
	ts := NewTokens(DefaultIdle, DefaultMax)
	tok, _, err := ts.Issue("d", RoleControl, t0)
	if err != nil {
		t.Fatal(err)
	}
	now := t0
	for now.Before(t0.Add(DefaultMax - 10*time.Minute)) {
		now = now.Add(10 * time.Minute)
		if _, err := ts.Check(tok, now); err != nil {
			t.Fatalf("at %v: %v", now.Sub(t0), err)
		}
	}
	if _, err := ts.Check(tok, t0.Add(DefaultMax)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("at max: %v, want ErrTokenExpired", err)
	}
}

func TestTokenKeepsNoPlainText(t *testing.T) {
	ts := NewTokens(DefaultIdle, DefaultMax)
	tok, _, err := ts.Issue("d", RoleControl, t0)
	if err != nil {
		t.Fatal(err)
	}
	for h, s := range ts.byHash {
		if strings.Contains(string(h[:]), tok) || strings.Contains(s.ID, tok) {
			t.Fatal("the token is kept in plain text")
		}
	}
}
