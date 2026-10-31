package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-36: t switches AGENTS between the cards and a dense task list:
// one row per step in wave order (state, title, model, time / tokens),
// the selected row marked; t again shows the cards.
func TestAgentsTaskListToggle(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t, agentsRunMsgs("r1", ai.RunStateRunning, agentsDiamondSteps())...)
	m.ai.runMsg, m.ai.streaming = 1, true
	cards := agentsText(m, 64, 40)
	m.agentsKey(t, "t")
	if !m.ai.agents.list {
		t.Fatal("t did not turn the list on")
	}
	got := agentsText(m, 64, 40)
	wantAll(t, got, "Tasks · 5 steps · 4 waves", "1 ✓ Planner", "2 ✓ Map", "3 ● Left", "3 ● Right", "4 ○ Verify", "!┌", "!Dependencies")
	lines := strings.Split(got, "\n")
	var left, mapRow string
	for _, l := range lines {
		if strings.Contains(l, "● Left") {
			left = l
		}
		if strings.Contains(l, "✓ Map") {
			mapRow = l
		}
	}
	if !strings.Contains(left, "▸ 3 ● Left") {
		t.Errorf("the running step is not marked as selected: %q", left)
	}
	if !strings.Contains(mapRow, "gpt-5-mini") || !strings.Contains(mapRow, "21 s") {
		t.Errorf("map row %q", mapRow)
	}
	m.agentsKey(t, "tab")
	if got := agentsText(m, 64, 40); !strings.Contains(got, "▸ 3 ● Right") {
		t.Errorf("Tab did not move in the list:\n%s", got)
	}
	m.agentsKey(t, "t")
	if back := agentsText(m, 64, 40); m.ai.agents.list || !strings.Contains(back, "┌") || back == "" {
		t.Fatalf("t did not bring the cards back:\n%s", back)
	}
	_ = cards
}
