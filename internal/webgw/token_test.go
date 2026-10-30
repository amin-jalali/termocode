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
