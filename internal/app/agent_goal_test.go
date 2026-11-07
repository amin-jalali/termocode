package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentgoal"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// goalTestModel is limitTestModel with the check spawn faked: every run
// lands in *spawned and gets buffer 50.
func goalTestModel(t *testing.T) (m *Model, screen *string, typed *[]string, spawned *[]string) {
	t.Helper()
	m, screen, typed = limitTestModel(t)
	var runs []string
	spawned = &runs
	old := agentGoalSpawn
	agentGoalSpawn = func(_ *Model, agentBuf int, title string, g *agentgoal.Goal) (int, error) {
		runs = append(runs, g.Verify+" @ "+g.Dir)
		return 50, nil
	}
	t.Cleanup(func() { agentGoalSpawn = old })
	return m, screen, typed, spawned
}

// startGoal walks the palette row, the prompt and the card.
func startGoal(t *testing.T, m *Model, line, button string) {
	t.Helper()
	m.dispatchTasksPalette(agentGoalPaletteID)
	if !m.promptOpen || m.promptKind != promptKindAgentGoalVerify {
		t.Fatalf("no check prompt (open %v kind %v)", m.promptOpen, m.promptKind)
	}
	if _, ok := m.handleAIPromptSubmit(line); !ok {
		t.Fatal("prompt not routed")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAgentGoalStart {
		t.Fatalf("no start card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	if _, ok := m.handleAIConfirm(button); !ok {
		t.Fatal("card not routed")
	}
}

// Spec (CMP-19): Cancel on the start card starts nothing; a rounds
// button starts the loop on the tab (named "Codex goal 0/3") and, while
// the agent still works, runs no check yet.
func TestAgentGoalStartCancel(t *testing.T) {
	m, _, _, spawned := goalTestModel(t)
	startGoal(t, m, "go test ./...", "cancel")
	if m.terminalTabs[0].Goal != nil {
		t.Fatal("Cancel started a loop")
	}
	startGoal(t, m, "go test ./...", "r3")
	g := m.terminalTabs[0].Goal
	if g == nil || g.Rounds != 3 || g.Verify != "go test ./..." || g.Dir != aiWorkspaceRoot() {
		t.Fatalf("goal = %+v", g)
	}
	if name := m.terminalTabs[0].agentTabName(); name != "Codex goal 0/3" {
		t.Errorf("tab name %q", name)
	}
	if len(*spawned) != 0 {
		t.Errorf("a check ran while the agent works: %v", *spawned)
	}
	m.promptOpen = false
	m.dispatchTasksPalette(agentGoalPaletteID)
	if m.promptOpen && m.promptKind == promptKindAgentGoalVerify {
		t.Error("a second loop could be started")
	}
}

// Spec (CMP-19): the agent's turn end (its screen goes to done) runs the
// check once; a failed check types one line with the failure, then Enter,
// and counts a round; a passing check ends the loop with a toast.
func TestAgentGoalLoopUntilPass(t *testing.T) {
	m, screen, typed, spawned := goalTestModel(t)
	startGoal(t, m, "make check", "r5")
	*screen = limitScreen(t, "codex-done.txt")
	m.onAgentOutput(7)
	if len(*spawned) != 1 || !strings.HasPrefix((*spawned)[0], "make check @ ") {
		t.Fatalf("turn end ran %v", *spawned)
	}
	m.agentGoalTurnEnded(7) // a second turn end while the check runs
	if len(*spawned) != 1 {
		t.Fatalf("two checks at once: %v", *spawned)
	}
	m.toast = toast.New()
	m.onTaskExit(50, 2)
	if len(*typed) != 2 || (*typed)[1] != "\r" || strings.Contains((*typed)[0], "\n") ||
		!strings.Contains((*typed)[0], "`make check` failed (exit 2, round 1 of 5)") {
		t.Fatalf("typed %q", *typed)
	}
	if g := m.terminalTabs[0].Goal; g == nil || g.Round != 1 || g.Verifying {
		t.Fatalf("after round 1: %+v", g)
	}
	if !strings.Contains(toastText(*m), "goal 1/5") {
		t.Errorf("toast %q", toastText(*m))
	}
	m.agentGoalTurnEnded(7)
	m.toast = toast.New()
	m.onTaskExit(50, 0)
	if m.terminalTabs[0].Goal != nil || len(*typed) != 2 {
		t.Fatalf("pass did not end the loop: %+v typed %q", m.terminalTabs[0].Goal, *typed)
	}
	if !strings.Contains(toastText(*m), "Check passed for Codex after 1 round") {
		t.Errorf("toast %q", toastText(*m))
	}
}

// Spec (CMP-19): the last round's failure stops the loop with the
// reason; a question on the agent's screen types nothing and keeps the
// loop; a usage limit runs no check.
func TestAgentGoalGuardsAndCap(t *testing.T) {
	m, screen, typed, spawned := goalTestModel(t)
	startGoal(t, m, "make check", "r3")
	g := m.terminalTabs[0].Goal
	m.terminalTabs[0].AgentState = agents.Done

	*screen = limitScreen(t, "codex-waiting-command.txt")
	m.agentGoalTurnEnded(7)
	m.toast = toast.New()
	m.onTaskExit(50, 1)
	if len(*typed) != 0 || m.terminalTabs[0].Goal == nil || g.Round != 0 {
		t.Fatalf("typed over a question: %q goal %+v", *typed, g)
	}
	if !strings.Contains(toastText(*m), "asks something") {
		t.Errorf("toast %q", toastText(*m))
	}

	*screen = limitScreen(t, "codex-done.txt")
	g.Round = 3
	m.agentGoalTurnEnded(7)
	m.toast = toast.New()
	m.onTaskExit(50, 1)
	if m.terminalTabs[0].Goal != nil || len(*typed) != 0 {
		t.Fatalf("the cap did not stop the loop: typed %q", *typed)
	}
	if !strings.Contains(toastText(*m), "no rounds left") {
		t.Errorf("toast %q", toastText(*m))
	}

	startGoal(t, m, "make check", "r3")
	m.terminalTabs[0].LimitMsg, m.terminalTabs[0].LimitAt = "limit", clock.Now().Add(time.Hour)
	n := len(*spawned)
	m.agentGoalTurnEnded(7)
	if len(*spawned) != n {
		t.Error("a check ran while a usage limit is on")
	}
}

// Spec (CMP-19): Agent: Stop Check Loop ends the loop; both rows are in
// the palette and the AI may never run them nor set the token cap.
func TestAgentGoalStopAndPolicy(t *testing.T) {
	m, _, _, _ := goalTestModel(t)
	startGoal(t, m, "make check", "r10")
	m.toast = toast.New()
	m.dispatchTasksPalette(agentGoalStopPaletteID)
	if m.terminalTabs[0].Goal != nil || !strings.Contains(toastText(*m), "Check loop stopped for Codex") {
		t.Fatalf("stop: goal %+v toast %q", m.terminalTabs[0].Goal, toastText(*m))
	}
	m.dispatchTasksPalette(agentGoalStopPaletteID)
	if !strings.Contains(toastText(*m), "No check loop runs") {
		t.Errorf("toast %q", toastText(*m))
	}
	for _, id := range []string{agentGoalPaletteID, agentGoalStopPaletteID} {
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v %v", id, p, ok)
		}
		found := false
		for _, it := range tasksPaletteItems() {
			found = found || it.ID == id
		}
		if !found {
			t.Errorf("%s has no palette row", id)
		}
	}
	if p, ok := aiSettingPolicies["agent_goal_max_tokens"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy %+v %v", p, ok)
	}
}
