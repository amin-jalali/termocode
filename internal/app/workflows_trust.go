package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// The trust pack of the run page (WFX-20, plan §4.4, §5.5, §6.6): Undo
// runs a write's stored recipe behind a card (Cancel first and
// focused); the day budget pauses a run with a card (Stop first,
// Continue second). Enter and Esc never do the risky thing.

// wfxUndoExec runs an undo's command (tests swap it).
var wfxUndoExec = spawnOutput

// wfxAskUndo asks before the first write of run id that is not undone.
func (m *Model) wfxAskUndo(id string) tea.Cmd {
	st := m.wfxS()
	rf, ok := m.wfxRunByID(id)
	if !ok || st.root == "" {
		return nil
	}
	if st.live[id] != nil {
		return m.aiInfo("Undo works once the run has ended")
	}
	for _, u := range wire.Undoable(st.root, rf) {
		if u.Undone {
			continue
		}
		st.pendingRun, st.pendingStep, st.pendingItem = id, u.Step, u.Item
		where := "Step " + u.Step
		if u.Item != "" {
			where = u.Step + " · item " + u.Item
		}
		m.wfxCard(confirmKindWorkflowUndo, "Undo this write?", where+"\n"+u.Target+"\n\nUndo: "+u.Title+
			"\nIt runs once; what was written stays in the run's history.",
			confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "undo", Title: "Undo", Style: confirm.StyleDestructive})
		return nil
	}
	return m.aiInfo("Nothing to undo in this run")
}

// answerWorkflowTrustCard answers the undo and budget cards. ok=false:
// not one of them.
func (m *Model) answerWorkflowTrustCard(kind confirmKindEnum, id string) (tea.Cmd, bool) {
	st := m.wfxS()
	runID, step, item := st.pendingRun, st.pendingStep, st.pendingItem
	switch kind {
	case confirmKindWorkflowUndo:
		st.pendingRun = ""
		if id != "undo" {
			return nil, true
		}
		rf, ok := m.wfxRunByID(runID)
		if !ok {
			return nil, true
		}
		u := wire.Undoer{Root: st.root, Workspace: st.ws, Exec: wfxUndoExec}
		if mc := newWFMCP(m.aiMCP(), m.aiMCPTrustAnnotations()); mc != nil {
			u.Tools = mc
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := u.Undo(ctx, rf, step, item); err != nil {
			return m.aiInfo("Undo failed: " + agentCtlShort(err.Error())), true
		}
		return m.aiInfo("Undone: " + step + " " + item), true
	case confirmKindWorkflowBudget:
		st.pendingRun = ""
		l := st.live[runID]
		if l == nil {
			return nil, true
		}
		if id == "continue" {
			l.budget, l.budgetOK = false, true
			l.h.Continue()
			return m.aiInfo(l.name + ": goes on past the day budget"), true
		}
		l.h.Cancel() // Stop: the safe answer
		return m.aiInfo(l.name + ": stopped at the day budget"), true
	}
	return nil, false
}

// wfxDayBudget is workflows_day_budget_usd (0: none).
func wfxDayBudget() float64 {
	v, _ := workflowSettingValue("setting-workflows-day-budget")
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// wfxDaySpent is what every workflow run that started today cost.
func (m *Model) wfxDaySpent() float64 {
	st := m.wfxS()
	now := clock.Now().In(wfxLoc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wfxLoc)
	seen := map[string]bool{}
	total := 0.0
	add := func(rf *run.RunFile) {
		if seen[rf.ID] || rf.Dry || rf.StartedAt.Before(day) {
			return
		}
		seen[rf.ID] = true
		total += wfxRunCost(rf)
	}
	for _, l := range st.live {
		add(l.h.Snapshot())
	}
	for _, list := range st.runs {
		for _, rf := range list {
			add(rf)
		}
	}
	return total
}

// wfxRunCost sums the step costs of a run, items too.
func wfxRunCost(rf *run.RunFile) float64 {
	var sum func(map[string]*run.StepState) float64
	sum = func(steps map[string]*run.StepState) float64 {
		t := 0.0
		for _, s := range steps {
			if s == nil {
				continue
			}
			if s.Output != nil {
				t += s.Output.CostUSD
			}
			if s.Each != nil {
				for _, it := range s.Each.Items {
					t += sum(it.Steps)
				}
			}
		}
		return t
	}
	return sum(rf.Steps)
}

// wfxCheckBudget pauses l when today's spend reached the day budget and
// asks; a run the user let go on is not asked again.
func (m *Model) wfxCheckBudget(l *wfxLive) {
	limit := wfxDayBudget()
	if l == nil || l.dry || l.budgetOK || l.budget || limit <= 0 {
		return
	}
	spent := m.wfxDaySpent()
	if spent < limit {
		return
	}
	l.budget = true
	l.h.Pause()
	if m.confirmOpen || m.promptOpen || m.pickerOpen {
		return // the PAUSED row says why; p continues
	}
	st := m.wfxS()
	st.pendingRun = l.id
	m.wfxCard(confirmKindWorkflowBudget, "Day budget reached",
		fmt.Sprintf("%s · workflows spent $%.2f today; the day budget is $%.2f (workflows_day_budget_usd).\n"+
			"The run waits before its next step. Stop ends it; finished items keep their results.", l.name, spent, limit),
		confirm.Button{ID: "stop", Title: "Stop"}, confirm.Button{ID: "continue", Title: "Continue"})
}
