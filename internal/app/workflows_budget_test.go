package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Spec (plan §4.4): limits.item_budget_usd and limits.budget_usd pause
// the run with the same card as the day budget (Stop first and focused,
// Continue second). An item over its budget asks once for that item; the
// run over its budget asks once for the run. The PAUSED row says which
// budget it was.
func TestWorkflowsRunAndItemBudgetsPause(t *testing.T) {
	m, _ := wfxRig(t)
	body := `{ "version": 1, "name": "capped", "title": "Capped",
  "limits": { "budget_usd": 1, "item_budget_usd": 0.5 },
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/capped.md", "content": "x" }
  ] }`
	if err := os.WriteFile(filepath.Join(wfxPrivateDir(), "capped.jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
	st := m.wfxS()
	e, ok := wire.Find(st.list, "capped")
	if !ok || e.Broken() {
		t.Fatalf("capped: %+v", e.Problems)
	}
	id, _, _, err := m.wfxStartID(e, false)
	if err != nil {
		t.Fatal(err)
	}
	wfxUntilGate(t, m, id)
	m.confirmOpen = false
	l := st.live[id]
	cost := func(top float64, items ...float64) *run.RunFile {
		rf := l.h.Snapshot()
		each := &run.EachState{}
		for i, c := range items {
			each.Items = append(each.Items, &run.ItemState{Key: []string{"LIN-1", "LIN-2"}[i], State: run.StateRunning,
				Steps: map[string]*run.StepState{"code": {State: run.StateDone, Output: &run.Output{CostUSD: c}}}})
		}
		rf.Steps = map[string]*run.StepState{"plan": {State: run.StateDone, Output: &run.Output{CostUSD: top}}, "fix": {State: run.StateRunning, Each: each}}
		return rf
	}
	card := func(want ...string) {
		t.Helper()
		if !l.h.Held() {
			t.Fatal("the run was not paused")
		}
		if !m.confirmOpen || m.confirmKind != confirmKindWorkflowBudget || m.confirm.SafeID() != "stop" {
			t.Fatalf("budget card: open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
		}
		box := ansi.Strip(m.confirm.Box())
		for _, w := range want {
			if !strings.Contains(box, w) {
				t.Errorf("card misses %q:\n%s", w, box)
			}
		}
	}

	m.wfxBudgetCheck(l, cost(0, 0.2, 0.3))
	if m.confirmOpen || l.h.Held() {
		t.Fatal("under every budget, the run was paused")
	}
	m.wfxBudgetCheck(l, cost(0, 0.6, 0.1))
	card("Item budget reached", "LIN-1", "$0.60", "$0.50", "item_budget_usd")
	st.detail = "run:" + id
	if page := wfxPage(m, 120, 40); !strings.Contains(page, "item budget") {
		t.Errorf("the PAUSED row does not name the item budget:\n%s", page)
	}
	m.answerWorkflowCard("continue")
	m.confirmOpen = false
	if l.h.Held() {
		t.Fatal("Continue did not let the run go on")
	}
	m.wfxBudgetCheck(l, cost(0, 0.7, 0.1))
	if m.confirmOpen || l.h.Held() {
		t.Fatal("an item the user let go on was asked about again")
	}
	m.wfxBudgetCheck(l, cost(0.5, 0.7, 0.4))
	card("Run budget reached", "$1.60", "$1.00", "budget_usd")
	m.answerWorkflowCard("stop")
	m.confirmOpen = false
	if rf := wfxPump(t, m, id, nil); rf.State != run.RunCancel {
		t.Fatalf("Stop on the run budget card: %s", rf.State)
	}
}
