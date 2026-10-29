package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// gateModel is a model with the frame gate on, as the real program runs.
func gateModel(t *testing.T) Model {
	t.Helper()
	withConfigDir(t)
	m := newTestModel(t)
	m.gate = &frameGate{on: true}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = nm.(Model)
	_ = m.View()
	return m
}

// gateCmdMsgs runs cmd (and batches) and returns every message it produced.
func gateCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, gateCmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestFrameGateDrawsOncePerFrame: Bubble Tea calls View after every
// message. Holding a key brings several messages per key (the key, nvim
// redraws, state replies), and drawing the whole screen for each one used
// most of a CPU core, so keys queued up and kept moving the cursor after
// they were let go. Within one frame (16 ms) View now returns the frame
// it already drew, and a tick draws the final state.
func TestFrameGateDrawsOncePerFrame(t *testing.T) {
	m := gateModel(t)
	first := m.View()

	// A change inside the same frame: not drawn yet, a tick is asked for.
	nm, cmd := m.Update(DevActionMsg{ID: "toast:gate-test"})
	m = nm.(Model)
	if m.View() != first {
		t.Fatal("redrew inside the same frame")
	}
	var tick tea.Msg
	for _, msg := range gateCmdMsgs(cmd) {
		if _, ok := msg.(frameTickMsg); ok {
			tick = msg
		}
	}
	if tick == nil {
		t.Fatal("no frame tick asked for: the last change would never be drawn")
	}

	// The tick draws the latest state.
	nm, _ = m.Update(tick)
	m = nm.(Model)
	if got := m.View(); got == first {
		t.Error("frame tick did not draw the change")
	}
}

// TestFrameGateIdleDrawsAtOnce: after a quiet moment a message is drawn
// at once (typing one key must not wait for a tick).
func TestFrameGateIdleDrawsAtOnce(t *testing.T) {
	m := gateModel(t)
	first := m.View()
	time.Sleep(20 * time.Millisecond)
	nm, _ := m.Update(DevActionMsg{ID: "toast:gate-test"})
	m = nm.(Model)
	if m.View() == first {
		t.Error("change after a quiet frame was not drawn at once")
	}
}

// TestFrameGateDrawsAttachAtOnce: a terminal change (a session client
// attached) and a resize draw at once, even inside a frame. The attach
// frame carries the mark the session server waits for; held back for a
// tick, the repaint went out before the mark and a new client saw a blank
// screen.
func TestFrameGateDrawsAttachAtOnce(t *testing.T) {
	// Applying a TerminalChangedMsg changes global terminal caps, so the
	// rule is checked directly; the resize goes through Update.
	if !drawAtOnce(TerminalChangedMsg{}) {
		t.Error("TerminalChangedMsg is held back for a tick")
	}
	m := gateModel(t)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}) // inside the frame
	m = nm.(Model)
	if m.gate.skip {
		t.Error("WindowSizeMsg held back for a tick")
	}
}
