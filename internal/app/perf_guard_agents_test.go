package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (U20 AP-9 perf guard): the AGENTS panel costs no CPU when idle.
// With a finished run shown, no 1 s tick is armed and idle frames reuse
// the drawn panel (no rebuild). A running chat whose panel is hidden
// arms no tick either: the tick only redraws a visible panel.
func TestPerfGuardAgentsPanelIdle(t *testing.T) {
	perfColour(t)
	m := perfModel(t, true)
	done := agentsDiamondSteps()
	for i := range done {
		done[i].State = "completed"
	}
	m.ai.conv.Messages = append(m.ai.conv.Messages, agentsRunMsgs("r1", ai.RunStateDone, done)...)
	m.actionsOpen, m.actionsPinned = true, true
	m.ai.agents.on = true
	if v := m.View(); !strings.Contains(v, "AGENTS") || !strings.Contains(v, "Verify") {
		t.Fatalf("AGENTS panel not shown:\n%s", v)
	}
	if c := m.aiActivityTick(); c != nil {
		t.Fatal("a tick is armed with no run going")
	}
	builds := agentsBuilds.Load()
	for i := 0; i < 100; i++ {
		m = cursorMove(m, i)
		next, _ := m.Update(aiActivityTickMsg{})
		m = next.(Model)
		if m.ai.act.ticking {
			t.Fatal("Update armed a tick while idle")
		}
		_ = m.View()
	}
	if n := agentsBuilds.Load() - builds; n != 0 {
		t.Errorf("idle frames rebuilt the AGENTS panel %d times", n)
	}

	// A run is going but the chat slot is closed: nothing to redraw.
	m.ai.streaming = true
	m.actionsOpen = false
	if c := m.aiActivityTick(); c != nil {
		t.Error("a tick is armed while the panel is hidden")
	}
	m.actionsOpen = true
	if c := m.aiActivityTick(); c == nil {
		t.Error("no tick while a run is going and the panel is shown")
	}
	m.ai.streaming = false
}
