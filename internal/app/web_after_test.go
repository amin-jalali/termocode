package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Spec (WEB-12): after each Update the registered publishers run while
// the hub runs (not at all without it); a publisher that panics is logged
// and the others still run; their Cmds come back batched.
func TestWebAfterUpdateRunsPublishers(t *testing.T) {
	m := aiTestModel(t)
	old := webAfterUpdates
	t.Cleanup(func() { webAfterUpdates = old })
	runs := 0
	webAfterUpdates = []func(*Model, tea.Msg) tea.Cmd{
		func(*Model, tea.Msg) tea.Cmd { panic("boom") },
		func(*Model, tea.Msg) tea.Cmd { runs++; return func() tea.Msg { return nil } },
	}
	if m.webAfterUpdate(nil) != nil || runs != 0 {
		t.Fatal("without a hub nothing runs")
	}
	webTestHub(t, &m)
	if m.webAfterUpdate(nil) == nil || runs != 1 {
		t.Fatalf("runs %d", runs)
	}
	errorRingMu.Lock()
	logged := strings.Join(errorRing, "\n")
	errorRingMu.Unlock()
	if !strings.Contains(logged, "[web] publish panic: boom") {
		t.Fatalf("errors.log misses the panic: %q", logged)
	}
}
