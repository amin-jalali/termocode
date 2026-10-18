package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// maxExtraAllocsPctCards is what four folded step cards and a run row may
// add to an AI panel frame, in percent of the plain chat's frame: the
// transcript is cached, so the cards cost (almost) nothing per frame.
const maxExtraAllocsPctCards = 10

// perfProfiles writes a two-step user profile, loads the profiles and
// makes it the active one (no run starts).
func perfProfiles(t *testing.T, m *Model) {
	t.Helper()
	path, err := ai.ProfilesPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"profiles": {"pipe": {"steps": [
		{"id": "a", "title": "Drafter", "category": "deep", "tools": {"preset": "read-only"}},
		{"id": "b", "title": "Fixer", "category": "quick"}
	]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.loadProfiles(ai.Credentials{})
	if len(m.ai.profiles.Errors) > 0 {
		t.Fatalf("profile errors: %v", m.ai.profiles.Errors)
	}
	m.ai.profile = "pipe"
	if p := m.ai.activeProfile(); p.Name != "pipe" || len(p.Steps) != 2 {
		t.Fatalf("active profile = %q with %d steps", p.Name, len(p.Steps))
	}
}

// perfRunTurn appends a finished two-step run (two folded cards each) as
// the chat's last two turns: four cards in all.
func perfRunTurn(m *Model) {
	u := &ai.Usage{Model: "claude-sonnet-4-5", Input: 3200, Output: 900, Requests: 2, PeakContext: 3200}
	cost := 0.0231
	for _, id := range []string{"r1", "r2"} {
		steps := []ai.StoredStep{
			{ID: "a", Title: "Drafter", Model: "claude-sonnet-4-5", State: string(agentdag.StateCompleted),
				Text: strings.Repeat("A finding with file:line. ", 20), Tools: []string{"⚙ Read a.go → 10 lines"},
				ElapsedMs: 14000, Usage: u, Cost: &cost},
			{ID: "b", Title: "Fixer", Model: "gpt-5-mini", State: string(agentdag.StateCompleted),
				Text: "Fixed it.", ElapsedMs: 22000, Usage: u, Cost: &cost, Files: 2},
		}
		m.ai.conv.Messages = append(m.ai.conv.Messages,
			ai.StoredMessage{Role: ai.StoredUser, Text: "review and fix"},
			ai.StoredMessage{Role: ai.StoredAssistant, Text: "I fixed the double confirm in `quit.go`.",
				Run: &ai.StoredRun{ID: id, Profile: "pipe", State: ai.RunStateDone, ElapsedMs: 36000}, Steps: steps})
	}
}

// Spec (U20 AP-7 perf guard): an idle chat with a pipeline profile
// loaded and no run costs no CPU: no 1 s activity tick is armed (and a
// stray tick does not re-arm), no run handle exists, and many idle frames
// start no goroutine.
func TestPerfGuardIdleProfileNoRun(t *testing.T) {
	perfColour(t)
	m := perfModel(t, true)
	perfProfiles(t, &m)
	perfRunTurn(&m)
	if !strings.Contains(m.View(), "pipe") {
		t.Fatal("profile chip not shown")
	}
	if m.ai.run != nil || m.ai.streaming {
		t.Fatal("a run is going with no message sent")
	}
	if c := m.aiActivityTick(); c != nil {
		t.Fatal("the activity tick is armed while idle")
	}
	if c := m.onAIActivityTick(); c != nil {
		t.Fatal("a stray activity tick re-armed itself while idle")
	}

	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		m = cursorMove(m, i)
		next, _ := m.Update(aiActivityTickMsg{})
		m = next.(Model)
		if m.ai.act.ticking {
			t.Fatal("Update re-armed the activity tick while idle")
		}
		_ = m.View()
	}
	runtime.GC()
	deadline := time.Now().Add(2 * time.Second)
	after := runtime.NumGoroutine()
	for after > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		after = runtime.NumGoroutine()
	}
	if after > before {
		t.Errorf("idle frames with a profile loaded: goroutines %d → %d", before, after)
	}
	if m.ai.run != nil || m.ai.streaming {
		t.Error("idle frames started a run")
	}
}

// Spec (U20 AP-7 perf guard): a chat frame with four folded step cards and
// two run rows costs at most 10 % more allocations than the plain chat.
func TestPerfGuardAIPanelFrameStepCards(t *testing.T) {
	perfColour(t)
	measure := func(cards bool) float64 {
		m := perfModel(t, true)
		if cards {
			perfProfiles(t, &m)
			perfRunTurn(&m)
			v := m.View()
			if !strings.Contains(v, "Drafter") || !strings.Contains(v, "run · 36 s") {
				t.Fatalf("step cards not shown:\n%s", v)
			}
		}
		_ = m.View()
		i := 0
		return testing.AllocsPerRun(50, func() {
			i++
			m = cursorMove(m, i)
			_ = m.View()
		})
	}
	plain := measure(false)
	with := measure(true)
	limit := plain * (1 + maxExtraAllocsPctCards/100.0)
	if with > limit || with > maxAllocsAIPanelFrame {
		t.Errorf("AI panel frame with 4 step cards: %.0f allocs, without: %.0f (limit %.0f, frame limit %d)",
			with, plain, limit, maxAllocsAIPanelFrame)
	} else {
		t.Logf("AI panel frame: %.0f allocs with 4 step cards, %.0f without", with, plain)
	}
}
