package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// SCN-32: an update installed while an AI turn runs. The restart card
// says so, and Restart waits for the turn: the turn ends, then
// termocode restarts. The run is not cut off.

// stubUpdateRestart counts restarts instead of re-executing.
func stubUpdateRestart(t *testing.T) *int {
	t.Helper()
	n := 0
	old := updateRestart
	updateRestart = func(*Model) tea.Cmd { n++; return nil }
	t.Cleanup(func() { updateRestart = old })
	return &n
}

// Spec: Given an AI turn that runs, when the restart card opens,
// then it says Restart waits for the turn.
func TestSCN32RestartCardNamesRunningTurn(t *testing.T) {
	m := aiTestModel(t)
	m.w, m.h = 160, 40
	m.aiS().streaming = true
	m.openUpdateRestartCard("v0.2.0")
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateRestart {
		t.Fatal("no restart card")
	}
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(ansi.Strip(m.confirm.Box()))), " ")
	if !strings.Contains(flat, "An AI turn is running") {
		t.Fatalf("card does not name the running turn:\n%s", flat)
	}
	if ids := buttonIDs(m.confirm); ids != "cancel,restart" {
		t.Fatalf("buttons %s (Later first)", ids)
	}
}

// Spec: Given an AI turn that runs, when the user picks Restart,
// then termocode does not restart yet and a toast says when it will;
// when the turn ends, termocode restarts once.
func TestSCN32RestartWaitsForAITurn(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	restarts := stubUpdateRestart(t)
	s.streaming = true
	m.openUpdateRestartCard("v0.2.0")
	m.confirmOpen = false
	m.handleConfirmSelect("restart")
	if *restarts != 0 {
		t.Fatal("restarted while the AI turn runs")
	}
	if got := toastText(m); !strings.Contains(got, "Restarts when the AI turn ends") {
		t.Fatalf("toast %q", got)
	}
	m.onAIChatDone(aiChatDoneMsg{gen: s.chatGen})
	if *restarts != 1 {
		t.Fatalf("restarts after the turn: %d, want 1", *restarts)
	}
	m.onAIChatDone(aiChatDoneMsg{gen: s.chatGen})
	if *restarts != 1 {
		t.Fatalf("a second turn end restarted again: %d", *restarts)
	}
}

// Spec: Given no AI turn, when the user picks Restart, then termocode
// restarts at once.
func TestSCN32RestartNowWithoutAITurn(t *testing.T) {
	m := aiTestModel(t)
	restarts := stubUpdateRestart(t)
	m.openUpdateRestartCard("v0.2.0")
	m.confirmOpen = false
	m.handleConfirmSelect("restart")
	if *restarts != 1 {
		t.Fatalf("restarts %d, want 1", *restarts)
	}
}
