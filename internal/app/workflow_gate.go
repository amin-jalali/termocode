package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
)

// The write gate card (WFX-6, plan §5.5, §6.3): every outside write of a
// workflow step asks first. Deny is first and focused; Enter and Esc
// deny. "Allow for this workflow" adds the step to the allow-list bound
// to the workflow file hash (internal/workflow/ledger), stored outside
// the repo. Kinds 3540–3549 are the WFX cards.
const confirmKindWorkflowGate confirmKindEnum = 3540

// WorkflowGateDecision is the answer to a write gate card.
type WorkflowGateDecision int

const (
	WorkflowGateDeny WorkflowGateDecision = iota
	WorkflowGateAllowOnce
	WorkflowGateAllowWorkflow
)

func (d WorkflowGateDecision) String() string {
	switch d {
	case WorkflowGateAllowOnce:
		return "allow once"
	case WorkflowGateAllowWorkflow:
		return "allow for this workflow"
	}
	return "deny"
}

// WorkflowWriteAsk is one write that waits for the user. Plain values
// only, so the engine does not depend on the app.
type WorkflowWriteAsk struct {
	Workflow string // the workflow name
	Step     string // the step id
	Item     string // the item key ("" for a step without items)
	Target   string // where it writes, e.g. "Linear · update_issue · LIN-228"
	Preview  string // what will be written
	Undo     string // how to take it back ("" when it cannot be undone)
	// StateRoot and FileHash bind "Allow for this workflow": without a
	// hash the card offers only Deny and Allow once. With a root the
	// card stores the allow-list itself (ledger.AllowStep).
	StateRoot, FileHash string
	// Answer gets the decision on the UI goroutine; it must not block.
	Answer func(WorkflowGateDecision)
}

// WorkflowWriteMsg asks a write through the program loop (the engine
// runs off the UI goroutine).
type WorkflowWriteMsg struct{ Ask WorkflowWriteAsk }

type workflowGateState struct {
	cur   *WorkflowWriteAsk
	queue []WorkflowWriteAsk
}

const (
	workflowGatePreviewLines = 12
	workflowGatePreviewBytes = 2000
)

// AskWorkflowWrite shows the gate card for a, or queues it while another
// card or dialog is open.
func (m *Model) AskWorkflowWrite(a WorkflowWriteAsk) tea.Cmd {
	if m.wfGate == nil {
		m.wfGate = &workflowGateState{}
	}
	m.wfGate.queue = append(m.wfGate.queue, a)
	m.showWaitingWorkflowGate()
	return nil
}

// showWaitingWorkflowGate opens the next waiting gate once no other
// dialog is open. A gate card that another card replaced without an
// answer counts as Deny. Update calls it after every message.
func (m *Model) showWaitingWorkflowGate() {
	g := m.wfGate
	if g == nil {
		return
	}
	if g.cur != nil {
		if m.confirmOpen && m.confirmKind == confirmKindWorkflowGate {
			return
		}
		m.finishWorkflowGate(WorkflowGateDeny)
	}
	if len(g.queue) == 0 || m.confirmOpen || m.promptOpen || m.pickerOpen {
		return
	}
	a := g.queue[0]
	g.queue = g.queue[1:]
	g.cur = &a
	actions := []confirm.Button{{ID: "once", Title: "Allow once"}}
	if a.FileHash != "" {
		actions = append(actions, confirm.Button{ID: "workflow", Title: "Allow for this workflow"})
	}
	m.confirm = confirm.Risky(a.Workflow+" wants to write", workflowGateBody(a),
		confirm.Button{ID: "deny", Title: "Deny"}, actions...).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindWorkflowGate
}

// workflowGateBody is the card text: where, what (redacted, capped) and
// the undo recipe.
func workflowGateBody(a WorkflowWriteAsk) string {
	var b strings.Builder
	b.WriteString("Step " + a.Step)
	if a.Item != "" {
		b.WriteString(" · item " + a.Item)
	}
	b.WriteString("\n" + ai.Redact(a.Target) + "\n")
	if p := strings.TrimRight(ai.Redact(a.Preview), "\n"); p != "" {
		cut := false
		if len(p) > workflowGatePreviewBytes {
			p, cut = strings.ToValidUTF8(p[:workflowGatePreviewBytes], ""), true
		}
		lines := strings.Split(p, "\n")
		if len(lines) > workflowGatePreviewLines {
			lines, cut = lines[:workflowGatePreviewLines], true
		}
		b.WriteString("\n    " + strings.Join(lines, "\n    ") + "\n")
		if cut {
			b.WriteString("    … (more in the run page)\n")
		}
	}
	if a.Undo != "" {
		b.WriteString("\nUndo: " + ai.Redact(a.Undo))
	} else {
		b.WriteString("\nThis write cannot be undone.")
	}
	return b.String()
}

// answerWorkflowGate handles a button of the card; any id but the two
// allow ids denies.
func (m *Model) answerWorkflowGate(id string) tea.Cmd {
	switch id {
	case "once":
		return m.finishWorkflowGate(WorkflowGateAllowOnce)
	case "workflow":
		return m.finishWorkflowGate(WorkflowGateAllowWorkflow)
	}
	return m.finishWorkflowGate(WorkflowGateDeny)
}

func (m *Model) finishWorkflowGate(d WorkflowGateDecision) tea.Cmd {
	if m.wfGate == nil || m.wfGate.cur == nil {
		return nil
	}
	a := *m.wfGate.cur
	m.wfGate.cur = nil
	var cmd tea.Cmd
	if d == WorkflowGateAllowWorkflow {
		if a.FileHash == "" {
			d = WorkflowGateAllowOnce
		} else if a.StateRoot != "" {
			if err := ledger.AllowStep(a.StateRoot, a.Workflow, a.FileHash, a.Step); err != nil {
				d = WorkflowGateAllowOnce // this write only; ask again next time
				cmd = m.aiInfo("Could not save the allow-list: " + agentCtlShort(err.Error()))
			}
		}
	}
	if a.Answer != nil {
		a.Answer(d)
	}
	return cmd
}
