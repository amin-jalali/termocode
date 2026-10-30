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

// Spec (web-easy): the hub runs in every termocode now, so with no
// browser, no gateway and nothing waiting the publishers run once more
// (to forget what they sent) and then not at all; a browser wakes them.
func TestWebAfterUpdateIdleHubDoesNothing(t *testing.T) {
	m := aiTestModel(t)
	old := webAfterUpdates
	t.Cleanup(func() { webAfterUpdates = old })
	runs := 0
	webAfterUpdates = []func(*Model, tea.Msg) tea.Cmd{func(*Model, tea.Msg) tea.Cmd { runs++; return nil }}
	webTestHub(t, &m)
	for i := 0; i < 5; i++ {
		m.webAfterUpdate(nil)
	}
	if runs != 1 {
		t.Fatalf("an idle hub ran the publishers %d times, want 1", runs)
	}
	m.web.live[&webConn{id: "c-1"}] = true
	m.webAfterUpdate(nil)
	m.webAfterUpdate(nil)
	if runs != 3 {
		t.Fatalf("with a browser: %d runs, want 3", runs)
	}
	delete(m.web.live, func() *webConn {
		for c := range m.web.live {
			return c
		}
		return nil
	}())
	m.webAfterUpdate(nil)
	m.webAfterUpdate(nil)
	if runs != 4 {
		t.Fatalf("after the browser left: %d runs, want 4", runs)
	}
}
