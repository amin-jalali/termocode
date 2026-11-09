package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// AI parity (workflows): workflow_control does the Workflows view's run
// keys. It asks exactly where the view asks: stop (x), on / off (o),
// retry (t) and undo (u) show a card there, so they ask every time with
// no Always allow; pause / continue (p) and the trigger list have none.

func wfxControlTool(t *testing.T, m *Model, ws string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(m.aiChatTools(ws, nil), "workflow_control")
	if !ok {
		t.Fatal("no workflow_control tool in the chat")
	}
	return tl
}

func TestAIWorkflowControlGateMatchesTheView(t *testing.T) {
	m, ws := wfxRig(t)
	tl := wfxControlTool(t, m, ws)
	for op, ask := range map[string]bool{"stop": true, "on": true, "off": true, "retry": true, "undo_write": true,
		"pause": false, "continue": false, "triggers": false} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s: ask=%v class=%q, want ask=%v and no Always allow", op, got, class, ask)
		}
	}
	if err := tl.Validate(map[string]any{"op": "dance"}); err == nil || !strings.Contains(err.Error(), "op") {
		t.Errorf("a bad op: %v", err)
	}
	if err := tl.Validate(map[string]any{"op": "retry"}); err == nil || !strings.Contains(err.Error(), "run") {
		t.Errorf("retry without a run: %v", err)
	}
	if err := tl.Validate(map[string]any{"op": "on", "name": "mine"}); err == nil || !strings.Contains(err.Error(), "no schedule") {
		t.Errorf("on for a workflow without a schedule: %v", err)
	}
	if _, ok := mcpServeRow("workflow_control"); ok {
		t.Error("outside agents get workflow_control")
	}
}

// pause, continue and stop act on the live run, as p and x do.
func TestAIWorkflowControlPauseContinueStop(t *testing.T) {
	m, ws := wfxRig(t)
	st := m.wfxS()
	st.sel = "mine"
	if _, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "pause", "name": "mine"}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("pause with no live run: %v", err)
	}
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	if card := wfxControlTool(t, m, ws).Describe(map[string]any{"op": "stop", "name": "mine"}); !strings.Contains(card, "Stop mine?") || !strings.Contains(card, "finished items keep their results") {
		t.Errorf("stop card: %s", card)
	}
	done := false
	rf := wfxPump(t, m, id, func(m *Model) {
		m.confirmOpen = false
		if done {
			m.answerWorkflowGate("deny")
			return
		}
		done = true
		l := m.wfxS().live[id]
		if out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "pause", "name": "mine"}); err != nil || !l.h.Held() {
			t.Fatalf("pause: %q %v held %v", out, err, l.h.Held())
		}
		if _, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "pause", "name": "mine"}); err == nil {
			t.Error("a paused run paused again")
		}
		if out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "continue", "name": "mine"}); err != nil || l.h.Held() {
			t.Fatalf("continue: %q %v held %v", out, err, l.h.Held())
		}
		if out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "stop", "name": "mine"}); err != nil || !strings.Contains(out, "Stopping mine") {
			t.Fatalf("stop: %q %v", out, err)
		}
		m.answerWorkflowGate("deny")
	})
	if rf.State == run.RunOK {
		t.Errorf("the stopped run ended %q", rf.State)
	}
}

// on and off switch the schedule (o), and say so on the card.
func TestAIWorkflowControlOnOff(t *testing.T) {
	m, ws := wfxRig(t)
	wfxAddScheduled(t, m, "tick", "", false)
	tl := wfxControlTool(t, m, ws)
	if card := tl.Describe(map[string]any{"op": "off", "name": "tick"}); !strings.Contains(card, "Turn tick off?") {
		t.Errorf("off card: %s", card)
	}
	if err := tl.Validate(map[string]any{"op": "on", "name": "tick"}); err == nil || !strings.Contains(err.Error(), "on already") {
		t.Errorf("on for an enabled workflow: %v", err)
	}
	if out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "off", "name": "tick"}); err != nil || !strings.Contains(out, "tick is off") {
		t.Fatalf("off: %q %v", out, err)
	}
	if e, _ := wire.Find(m.wfxS().list, "tick"); e.Enabled {
		t.Fatal("tick is still on")
	}
	if card := wfxControlTool(t, m, ws).Describe(map[string]any{"op": "on", "name": "tick"}); !strings.Contains(card, "runs by itself on its schedule") {
		t.Errorf("on card: %s", card)
	}
	if out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "on", "name": "tick"}); err != nil || !strings.Contains(out, "tick is on") {
		t.Fatalf("on: %q %v", out, err)
	}
	if e, _ := wire.Find(m.wfxS().list, "tick"); !e.Enabled {
		t.Fatal("tick is not on")
	}
}

// retry runs an ended run again from its failed part (t): the denied
// write asks again on its gate card.
func TestAIWorkflowControlRetry(t *testing.T) {
	m, ws := wfxRig(t)
	st := m.wfxS()
	st.sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	wfxPump(t, m, id, func(m *Model) { m.answerWorkflowGate("deny"); m.confirmOpen = false })
	card := wfxControlTool(t, m, ws).Describe(map[string]any{"op": "retry", "run": id})
	if !strings.Contains(card, "Retry the run of mine?") || !strings.Contains(card, "Runs again: rep") {
		t.Fatalf("retry card: %s", card)
	}
	out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "retry", "run": id})
	if err != nil || !strings.Contains(out, "Retrying run "+id) {
		t.Fatalf("retry: %q %v", out, err)
	}
	asked := 0
	end := wfxPump(t, m, id, func(m *Model) { asked++; m.answerWorkflowGate("once"); m.confirmOpen = false })
	if end.State != run.RunOK || end.Resumed != 1 || asked != 1 {
		t.Fatalf("after retry %q resumed %d asked %d", end.State, end.Resumed, asked)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "reports", "docs.md")); err != nil || string(b) != "2 docs" {
		t.Fatalf("the retried write: %q %v", b, err)
	}
}

// undo_write runs a write's stored undo once (u); the card names it.
func TestAIWorkflowControlUndoWrite(t *testing.T) {
	m, ws := wfxRig(t)
	var ran []string
	prev := wfxUndoExec
	wfxUndoExec = func(_ context.Context, argv []string, dir string) (string, int, error) {
		ran = append(ran, dir+": "+strings.Join(argv, " "))
		return "", 0, nil
	}
	t.Cleanup(func() { wfxUndoExec = prev })
	wfxStoredRun(t, m, "u1", "mine", 0, wfxTestNow.Add(-time.Hour))
	m.wfxLoad("")
	card := wfxControlTool(t, m, ws).Describe(map[string]any{"op": "undo_write", "run": "u1"})
	if !strings.Contains(card, "close PR #412") || !strings.Contains(card, "pr · item LIN-2") {
		t.Fatalf("undo card: %s", card)
	}
	out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "undo_write", "run": "u1"})
	if err != nil || !strings.Contains(out, "Undone") {
		t.Fatalf("undo: %q %v", out, err)
	}
	if len(ran) != 1 || ran[0] != ws+": gh pr close wf/lin-2 --delete-branch" {
		t.Fatalf("undo ran %q", ran)
	}
	if _, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "undo_write", "run": "u1"}); err == nil || !strings.Contains(err.Error(), "nothing to undo") {
		t.Fatalf("a second undo: %v", err)
	}
}

// triggers reads the trigger list at once (Workflows: Trigger Events…).
func TestAIWorkflowControlTriggers(t *testing.T) {
	m, ws := wfxRig(t)
	out, err := wfxToolRun(t, m, ws, "workflow_control", map[string]any{"op": "triggers"})
	if err != nil || !strings.Contains(out, "trigger(s) polled every") || !strings.Contains(out, "No trigger events yet") {
		t.Fatalf("triggers: %q %v", out, err)
	}
	if m.confirmOpen {
		t.Error("the trigger list opened a card for the user")
	}
}
