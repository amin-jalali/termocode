package agentgoal

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

// Spec (CMP-19): New trims the verify line, takes DefaultRounds for a
// bad count and sets the time cap to rounds × PerRound.
func TestNewDefaults(t *testing.T) {
	g := New("  go test ./...  ", "/w", 0, -3, 100, t0)
	if g.Verify != "go test ./..." || g.Rounds != DefaultRounds || g.Time != DefaultRounds*PerRound || g.Tokens != 0 || g.StartTokens != 100 {
		t.Fatalf("goal = %+v", g)
	}
	if g.Label() != "goal 0/5" {
		t.Fatalf("label = %q", g.Label())
	}
}

// Spec (CMP-19): exit 0 is Met; a failure retries until the rounds are
// used, then StopRounds. Next clears Verifying.
func TestNextRounds(t *testing.T) {
	g := New("make check", "/w", 2, 0, 0, t0)
	g.Verifying = true
	if o := g.Next(1, t0, -1); o != Retry || g.Verifying {
		t.Fatalf("first failure = %v (verifying %v)", o, g.Verifying)
	}
	g.Round = 2
	if o := g.Next(1, t0, -1); o != StopRounds || !o.Stopped() || o.Reason() == "" {
		t.Fatalf("after the last round = %v", o)
	}
	if o := g.Next(0, t0, -1); o != Met || o.Stopped() {
		t.Fatalf("exit 0 = %v", o)
	}
}

// Spec (CMP-19): the time cap and the token cap stop the loop; unknown
// tokens (< 0) never stop it; a new day (fewer tokens than the start)
// counts the new total alone.
func TestNextCaps(t *testing.T) {
	g := New("make check", "/w", 3, 1000, 5000, t0)
	if o := g.Next(2, t0.Add(59*time.Minute), -1); o != Retry {
		t.Fatalf("inside the caps = %v", o)
	}
	if o := g.Next(2, t0.Add(60*time.Minute), -1); o != StopTime {
		t.Fatalf("time over = %v", o)
	}
	if o := g.Next(2, t0, 6000); o != StopTokens {
		t.Fatalf("tokens over = %v", o)
	}
	if o := g.Next(2, t0, 5999); o != Retry {
		t.Fatalf("tokens under = %v", o)
	}
	if g.TokensUsed(300) != 300 {
		t.Fatalf("a new day counts the new total: %d", g.TokensUsed(300))
	}
}

// Spec (CMP-19): the typed prompt is one line (no newline can send it
// early), names the check, the exit and the round, keeps only the last
// TailLines non-blank lines, strips control characters and is capped.
func TestPrompt(t *testing.T) {
	var b strings.Builder
	for i := range 50 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\x1b\n\n")
	}
	b.WriteString("FAIL pkg/foo\n")
	p := Prompt("go test ./...", 1, 2, 5, b.String())
	if strings.ContainsAny(p, "\n\r\x1b") {
		t.Fatalf("prompt has a newline or control char: %q", p)
	}
	for _, w := range []string{"`go test ./...`", "exit 1", "round 2 of 5", "FAIL pkg/foo"} {
		if !strings.Contains(p, w) {
			t.Fatalf("prompt misses %q: %q", w, p)
		}
	}
	if n := strings.Count(p, " | "); n != TailLines-1 {
		t.Fatalf("joined %d lines, want %d", n+1, TailLines)
	}
	long := Prompt("c", 2, 1, 3, strings.Repeat("é", 4000))
	if len(long) > TailBytes+300 || !strings.Contains(long, "… ") {
		t.Fatalf("long output not capped: %d bytes", len(long))
	}
	if p := Prompt("c", 2, 1, 3, " \n "); strings.Contains(p, "Last output") {
		t.Fatalf("empty output still named: %q", p)
	}
}
