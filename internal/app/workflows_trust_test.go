package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// The trust pack on the run page (WFX-20, plan §4.4, §5.5): the UNDO
// list shows each write that can be taken back; u asks on a card
// (Cancel first and focused) and runs the stored recipe once. The day
// budget (workflows_day_budget_usd) pauses a run that would spend past
// it, with a card where Stop is first and Continue second.

// wfxStoredRun writes a finished run of workflow name with cost and a
// pull request write that has an undo recipe.
func wfxStoredRun(t *testing.T, m *Model, id, name string, cost float64, started time.Time) {
	t.Helper()
	root := m.wfxS().root
	rf := run.RunFile{Version: 1, ID: id, Workflow: name, State: run.RunOK, StartedAt: started, EndedAt: started.Add(time.Minute),
		Steps: map[string]*run.StepState{"code": {State: run.StateDone, Output: &run.Output{CostUSD: cost}}}}
	b, _ := json.Marshal(rf)
	if err := (wire.RunFiles{Root: root}).WriteRunFile(id, b); err != nil {
		t.Fatal(err)
	}
	l, err := ledger.Open(root, name, id)
	if err != nil {
		t.Fatal(err)
	}
	ok := func(context.Context, string) (ledger.Result, error) { return ledger.Result{ExternalID: "#412"}, nil }
	if _, _, err := l.Do(context.Background(), ledger.Write{Step: "pr", Item: "LIN-2", Target: "github · pr #412",
		Undo: &ledger.Undo{Title: "close PR #412 and delete its branch", Tool: "github.pr.close", Args: map[string]string{"branch": "wf/lin-2", "delete_branch": "true"}}}, ok, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowsUndoCard(t *testing.T) {
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
	m.wfxShowPage("run:u1", false)
	if page := wfxPage(m, 120, 40); !strings.Contains(page, "UNDO") || !strings.Contains(page, "close PR #412") {
		t.Fatalf("the run page lists the undo:\n%s", page)
	}
	m.handleWorkflowsPageKey(wfxKey('u'))
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowUndo || m.confirm.SafeID() != "cancel" {
		t.Fatalf("undo card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "close PR #412") || !strings.Contains(box, "pr · item LIN-2") {
		t.Fatalf("card:\n%s", box)
	}
	m.answerWorkflowCard("cancel")
	if len(ran) != 0 {
		t.Fatal("Cancel ran the undo")
	}
	m.handleWorkflowsPageKey(wfxKey('u'))
	cmd := m.answerWorkflowCard("undo")
	wfxRunCmd(cmd)
	if len(ran) != 1 || ran[0] != ws+": gh pr close wf/lin-2 --delete-branch" {
		t.Fatalf("undo ran %q", ran)
	}
	if page := wfxPage(m, 120, 40); !strings.Contains(page, "↶ undone") {
		t.Fatalf("the row shows undone:\n%s", page)
	}
	m.handleWorkflowsPageKey(wfxKey('u'))
	if m.confirmOpen && m.confirmKind == confirmKindWorkflowUndo {
		t.Fatal("an undone write asks again")
	}
}

func TestWorkflowsDayBudgetPauses(t *testing.T) {
	m, _ := wfxRig(t)
	if err := saveHTTPSetting("workflows_day_budget_usd", 0.5); err != nil {
		t.Fatal(err)
	}
	wfxStoredRun(t, m, "b1", "mine", 0.3, wfxTestNow.Add(-2*time.Hour))
	wfxStoredRun(t, m, "b2", "docs-report", 0.3, wfxTestNow.Add(-time.Hour))
	wfxStoredRun(t, m, "old", "mine", 5, wfxTestNow.Add(-30*time.Hour)) // yesterday: not counted
	m.wfxLoad("")
	if spent := m.wfxDaySpent(); spent < 0.59 || spent > 0.61 {
		t.Fatalf("spent today %v", spent)
	}
	m.wfxS().sel = "mine"
	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id := wfxOnlyLive(t, m)
	l := m.wfxS().live[id]
	if !l.h.Held() {
		t.Fatal("a run past the day budget was not paused")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowBudget || m.confirm.SafeID() != "stop" {
		t.Fatalf("budget card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	m.answerWorkflowCard("continue")
	if l.h.Held() {
		t.Fatal("Continue did not let the run go on")
	}
	rf := wfxPump(t, m, id, func(m *Model) { m.answerWorkflowGate("deny"); m.confirmOpen = false })
	if rf.State == run.RunCancel {
		t.Fatal("Continue stopped the run")
	}

	m.answerWorkflowCardKind(confirmKindWorkflowRun, "run")
	id = wfxOnlyLive(t, m)
	m.answerWorkflowCard("stop")
	if rf := wfxPump(t, m, id, nil); rf.State != run.RunCancel {
		t.Fatalf("Stop on the budget card: %s", rf.State)
	}
}
