package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
)

func gateAsk(got *[]WorkflowGateDecision) WorkflowWriteAsk {
	return WorkflowWriteAsk{
		Workflow: "weekday-bugs", Step: "state", Item: "LIN-228",
		Target:  "Linear · update_issue · LIN-228",
		Preview: "state: Todo → In Review",
		Undo:    "update_issue state → Todo",
		Answer:  func(d WorkflowGateDecision) { *got = append(*got, d) },
	}
}

// Spec (WFX-6, plan §5.5): the write gate card shows workflow, step,
// item, target and the text to be written; Deny is first and focused,
// then Allow once, then Allow for this workflow.
func TestWorkflowGateCardOrder(t *testing.T) {
	m := aiTestModel(t)
	var got []WorkflowGateDecision
	a := gateAsk(&got)
	a.FileHash = ledger.FileHash([]byte("x"))
	m.AskWorkflowWrite(a)
	if !m.confirmOpen || m.confirmKind != confirmKindWorkflowGate || m.confirm.SafeID() != "deny" {
		t.Fatalf("open=%v kind=%v safe=%q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	var titles []string
	for _, b := range m.confirm.Buttons() {
		titles = append(titles, b.Title)
		if b.Style != confirm.StyleDefault {
			t.Errorf("button %q is styled %q; no answer is the default look", b.Title, b.Style)
		}
	}
	if strings.Join(titles, "|") != "Deny|Allow once|Allow for this workflow" {
		t.Fatalf("buttons %v", titles)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, s := range []string{"weekday-bugs wants to write", "state", "LIN-228", "update_issue", "Todo → In Review", "Undo: update_issue state → Todo"} {
		if !strings.Contains(box, s) {
			t.Errorf("card misses %q:\n%s", s, box)
		}
	}
}

// Spec: Enter on a fresh card and Esc both deny.
func TestWorkflowGateEnterAndEscDeny(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc} {
		m := aiTestModel(t)
		var got []WorkflowGateDecision
		m.AskWorkflowWrite(gateAsk(&got))
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(tea.KeyMsg{Type: key})
		if cmd == nil {
			t.Fatalf("%v: no message", key)
		}
		next, _ := m.Update(cmd())
		m = next.(Model)
		if len(got) != 1 || got[0] != WorkflowGateDeny {
			t.Fatalf("%v: answers %v", key, got)
		}
		if m.confirmOpen {
			t.Fatalf("%v: card still open", key)
		}
	}
}

// Spec: Allow once lets this write go and stores nothing; Allow for this
// workflow adds the step to the allow-list bound to the file hash.
func TestWorkflowGateAllowAnswers(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	hash := ledger.FileHash([]byte(`{"name":"weekday-bugs"}`))
	var got []WorkflowGateDecision
	a := gateAsk(&got)
	a.StateRoot, a.FileHash = root, hash
	m.AskWorkflowWrite(a)
	m.answerWorkflowGateForTest("once")
	if ok, _ := ledger.Allowed(root, "weekday-bugs", hash, "state"); ok {
		t.Fatal("Allow once stored an allow-list")
	}
	m.AskWorkflowWrite(a)
	m.answerWorkflowGateForTest("workflow")
	if len(got) != 2 || got[0] != WorkflowGateAllowOnce || got[1] != WorkflowGateAllowWorkflow {
		t.Fatalf("answers %v", got)
	}
	if ok, _ := ledger.Allowed(root, "weekday-bugs", hash, "state"); !ok {
		t.Fatal("Allow for this workflow not stored")
	}
	if _, err := os.Stat(filepath.Join(root, "weekday-bugs.state.json")); err != nil {
		t.Fatal(err)
	}
	// Without a file hash there is nothing to bind to: no third button.
	m.AskWorkflowWrite(gateAsk(&got))
	if n := len(m.confirm.Buttons()); n != 2 {
		t.Fatalf("%d buttons without a hash", n)
	}
}

// Spec: a gate that comes while another card is open waits behind it,
// and a second gate waits behind the first.
func TestWorkflowGateWaitsForOtherCard(t *testing.T) {
	m := aiTestModel(t)
	m.confirm = confirm.New("Other card", "x", []confirm.Button{{ID: "ok", Title: "OK"}})
	m.confirmOpen, m.confirmKind = true, confirmKindHotExit
	var got []WorkflowGateDecision
	m.AskWorkflowWrite(gateAsk(&got))
	b := gateAsk(&got)
	b.Item = "LIN-230"
	m.AskWorkflowWrite(b)
	if m.confirmKind != confirmKindHotExit {
		t.Fatal("the other card was replaced")
	}
	m.confirmOpen = false
	m.showWaitingWorkflowGate()
	if m.confirmKind != confirmKindWorkflowGate || !strings.Contains(ansi.Strip(m.confirm.Box()), "LIN-228") {
		t.Fatal("first gate not shown")
	}
	m.answerWorkflowGateForTest("deny")
	m.showWaitingWorkflowGate()
	if !m.confirmOpen || !strings.Contains(ansi.Strip(m.confirm.Box()), "LIN-230") {
		t.Fatal("second gate not shown")
	}
}

// Spec: a gate card replaced by another card without an answer counts as
// Deny (the run never waits for a card that is gone).
func TestWorkflowGateReplacedCountsAsDeny(t *testing.T) {
	m := aiTestModel(t)
	var got []WorkflowGateDecision
	m.AskWorkflowWrite(gateAsk(&got))
	m.confirm = confirm.New("Other card", "x", []confirm.Button{{ID: "ok", Title: "OK"}})
	m.confirmKind = confirmKindHotExit
	m.showWaitingWorkflowGate()
	if len(got) != 1 || got[0] != WorkflowGateDeny {
		t.Fatalf("answers %v", got)
	}
}

// Spec: the preview is redacted and capped.
func TestWorkflowGatePreviewRedactedAndCapped(t *testing.T) {
	m := aiTestModel(t)
	var got []WorkflowGateDecision
	a := gateAsk(&got)
	tok := "sk-" + "test0123456789abcdefghijklmnop"
	a.Preview = "token " + tok + "\n" + strings.Repeat("line\n", 200)
	m.AskWorkflowWrite(a)
	box := ansi.Strip(m.confirm.Box())
	if strings.Contains(box, tok) {
		t.Fatal("secret on the card")
	}
	if n := strings.Count(workflowGateBody(a), "line"); n > workflowGatePreviewLines {
		t.Fatalf("%d preview lines", n)
	}
}

// Golden (WFX-6): the card at 80×24.
func TestWorkflowGateCardGolden(t *testing.T) {
	m := aiTestModel(t)
	m.w, m.h = 80, 24
	var got []WorkflowGateDecision
	a := gateAsk(&got)
	a.FileHash = "sha256:x"
	m.AskWorkflowWrite(a)
	gotBox := ansi.Strip(m.confirm.Box()) + "\n"
	wd, _ := os.Getwd()
	golden := filepath.Join(wd, "testdata", "workflow_gate_card.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(gotBox), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no golden %s: %v", golden, err)
	}
	if string(want) != gotBox {
		t.Errorf("card changed.\n--- want\n%s\n--- got\n%s", want, gotBox)
	}
}

func (m *Model) answerWorkflowGateForTest(id string) {
	m.confirmOpen = false
	_ = m.handleConfirmSelect(id)
}
