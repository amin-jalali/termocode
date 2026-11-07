package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/toast"
)

// acceptOn turns auto-accept on for tab 0 through the card.
func acceptOn(t *testing.T, m *Model) {
	t.Helper()
	m.dispatchTasksPalette(agentAutoAcceptPaletteID)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentAutoAccept {
		t.Fatalf("no card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false // the update loop closes the card first
	m.handleConfirmSelect("on")
	if !m.terminalTabs[0].AutoAccept {
		t.Fatal("Turn on did not turn it on")
	}
}

// Spec (CMP-64): Agent: Toggle Auto-Accept is listed, the AI may never
// run it, and turning it on opens a Cancel-first card; Cancel (or Esc)
// leaves it off. Turning it off needs no card. A tab that is not an
// agent, or an agent whose prompts termocode cannot read, gets a toast.
func TestAgentAutoAcceptToggle(t *testing.T) {
	m, _, _ := limitTestModel(t)
	if p, ok := aiActionPolicyFor(agentAutoAcceptPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentAutoAcceptPaletteID && it.Title == "Agent: Toggle Auto-Accept" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	m.dispatchTasksPalette(agentAutoAcceptPaletteID)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentAutoAccept {
		t.Fatal("no card")
	}
	if b := m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
		t.Errorf("card is not Cancel-first: %+v", b)
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if m.terminalTabs[0].AutoAccept {
		t.Error("Cancel turned it on")
	}
	acceptOn(t, m)
	if got := m.panelEntryName(panelBarEntry{Kind: panelKindTerminal, Index: 0}); !strings.Contains(got, "auto-yes") {
		t.Errorf("tab name %q", got)
	}
	m.dispatchTasksPalette(agentAutoAcceptPaletteID)
	if m.confirmOpen || m.terminalTabs[0].AutoAccept {
		t.Error("turning off opened a card or stayed on")
	}
	m.terminalTabs[0].Agent = "amp"
	m.toast = toast.New()
	m.dispatchTasksPalette(agentAutoAcceptPaletteID)
	if m.confirmOpen || !strings.Contains(toastText(*m), "cannot read") {
		t.Errorf("amp: open %v toast %q", m.confirmOpen, toastText(*m))
	}
	m.terminalTabs[0].Agent = ""
	m.toast = toast.New()
	m.dispatchTasksPalette(agentAutoAcceptPaletteID)
	if !strings.Contains(toastText(*m), "agent tab") {
		t.Errorf("no agent: %q", toastText(*m))
	}
}

// Spec (CMP-64): with auto-accept on, a permission prompt is answered
// once (Enter on "Yes"), no "waiting for you" toast; the same prompt on
// screen again is not answered twice; after the agent works again the
// next prompt is answered. Off (the default), nothing is typed.
func TestAgentAutoAcceptAnswersOnce(t *testing.T) {
	m, screen, typed := limitTestModel(t)
	*screen = limitScreen(t, "codex-waiting-command.txt")
	m.onAgentOutput(7)
	if len(*typed) != 0 {
		t.Fatalf("typed while off: %q", *typed)
	}
	m.terminalTabs[0].AgentState = agents.Working
	acceptOn(t, m)
	m.toast = toast.New()
	m.onAgentOutput(7)
	if strings.Join(*typed, "") != "\r" {
		t.Fatalf("typed %q", *typed)
	}
	if txt := toastText(*m); strings.Contains(txt, "waiting for you") || !strings.Contains(txt, "Auto-accepted") {
		t.Errorf("toast %q", txt)
	}
	m.onAgentOutput(7)
	if len(*typed) != 1 {
		t.Errorf("answered twice: %q", *typed)
	}
	*screen = limitScreen(t, "codex-working.txt")
	m.onAgentOutput(7)
	*screen = limitScreen(t, "codex-waiting-command.txt")
	m.onAgentOutput(7)
	if len(*typed) != 2 {
		t.Errorf("next prompt not answered: %q", *typed)
	}
}

// Spec (CMP-64): a risky prompt (here rm -rf) and a question with its own
// choices are never answered; the risky one says why in a toast and the
// user is told the agent waits.
func TestAgentAutoAcceptSkipsRiskyAndQuestions(t *testing.T) {
	m, screen, typed := limitTestModel(t)
	m.terminalTabs[0].Agent, m.terminalTabs[0].Name = "claude", "claude"
	acceptOn(t, m)
	m.toast = toast.New()
	*screen = limitScreen(t, "claude-waiting-bash.txt")
	m.onAgentOutput(7)
	if len(*typed) != 0 {
		t.Errorf("risky prompt answered: %q", *typed)
	}
	if txt := toastText(*m); !strings.Contains(txt, "rm -r") {
		t.Errorf("toast %q", txt)
	}
	m.terminalTabs[0].AgentState = agents.Working
	*screen = limitScreen(t, "claude-waiting-question.txt")
	m.onAgentOutput(7)
	if len(*typed) != 0 {
		t.Errorf("question answered: %q", *typed)
	}
}

// Spec (CMP-64): auto-accept turns itself off after agentAutoAcceptMax
// answers and says so.
func TestAgentAutoAcceptCap(t *testing.T) {
	m, screen, typed := limitTestModel(t)
	acceptOn(t, m)
	for i := 0; i < agentAutoAcceptMax+3; i++ {
		*screen = limitScreen(t, "codex-working.txt")
		m.onAgentOutput(7)
		*screen = limitScreen(t, "codex-waiting-command.txt")
		m.onAgentOutput(7)
	}
	if len(*typed) != agentAutoAcceptMax {
		t.Errorf("typed %d times, want %d", len(*typed), agentAutoAcceptMax)
	}
	if m.terminalTabs[0].AutoAccept {
		t.Error("still on after the cap")
	}
}
