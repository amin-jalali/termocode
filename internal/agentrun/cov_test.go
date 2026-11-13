package agentrun

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestStallTextUnits(t *testing.T) {
	for d, want := range map[time.Duration]string{
		2 * time.Minute:         "2 min",
		90 * time.Second:        "1m30s",
		1500 * time.Millisecond: "2s",
		250 * time.Millisecond:  "250ms",
	} {
		if got := StallText(d); got != want {
			t.Errorf("StallText(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestMessagesToAStepAreChecked(t *testing.T) {
	h := &Handle{}
	if err := h.Send("n1", "   "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty: %v", err)
	}
	if err := h.Send("n1", strings.Repeat("é", SendMax+1)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("too long: %v", err)
	}
	if _, err := h.TeamSend("a", "b", " "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("team empty: %v", err)
	}
	if orAll("") != "all" || orAll("x") != "x" {
		t.Error("orAll")
	}
}

func TestTextCutsKeepRunesWhole(t *testing.T) {
	if got := capInput("ééé", 3); got != "é"+CutNote {
		t.Errorf("capInput = %q", got)
	}
	if capInput("ab", 5) != "ab" {
		t.Error("a short input is kept")
	}
	if cutRunes("héllo", 3) != "hé…" || cutRunes("hé", 3) != "hé" {
		t.Error("cutRunes")
	}
	msgs := []ai.Message{{Role: ai.RoleAssistant, Content: " first "}, {Role: ai.RoleUser, Content: "q"}}
	if finalText(msgs) != "first" {
		t.Errorf("finalText %q", finalText(msgs))
	}
	if finalText([]ai.Message{{Role: ai.RoleUser, Content: "q"}}) != "" || finalText(nil) != "" {
		t.Error("no assistant message, no text")
	}
}

func TestGateAcquireStopsWhenContextEnds(t *testing.T) {
	var g Gate
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Acquire(dead); err == nil {
		t.Error("a dead context must not take the gate")
	}
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	// the gate is held now; a waiting caller leaves when its context ends
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if err := g.Acquire(ctx); err == nil {
		t.Error("a held gate must make the caller wait until its context ends")
	}
	if g.TryAcquire() {
		t.Error("the gate is still held")
	}
}
