package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Splitter drags (explorer, AI / actions panel, bottom panel) used to send
// a synchronous nvim_ui_try_resize (plus a Lua window resize for the
// panel) on EVERY mouse-motion event. Each is an RPC round trip and makes
// nvim redraw the whole grid, so a fast drag queued dozens of them and the
// splitter lagged behind the pointer.
//
// Now the chrome layout still follows every motion event (cheap, Go
// only), while the nvim side is coalesced: during a drag at most one
// resize per resizeInterval is sent, always with the latest size, and the
// final size is sent the moment the drag ends.

const resizeInterval = 33 * time.Millisecond // ≤ 30 resizes per second

// resizeThrottle coalesces nvim resize requests.
type resizeThrottle struct {
	last      time.Time // when the last resize was sent
	pending   bool      // a newer size waits to be sent
	scheduled bool      // a flush tick is already queued
	w, h      int       // latest requested size
	panel     bool      // the bottom panel height must be re-applied too
}

// request records a new size. It returns true when the caller should send
// now (not dragging, or the interval has passed); otherwise the size is
// kept pending for the next flush.
func (t *resizeThrottle) request(w, h int, dragging bool, now time.Time) bool {
	t.w, t.h = w, h
	if !dragging || now.Sub(t.last) >= resizeInterval {
		t.pending = false
		t.last = now
		return true
	}
	t.pending = true
	return false
}

// flush returns the pending size (ok=false when nothing waits).
func (t *resizeThrottle) flush(now time.Time) (w, h int, ok bool) {
	t.scheduled = false
	if !t.pending {
		return 0, 0, false
	}
	t.pending = false
	t.last = now
	return t.w, t.h, true
}

type resizeFlushMsg struct{}

// sendNvimResize applies the editor size to nvim, coalesced while a
// splitter drag is in progress.
func (m *Model) sendNvimResize(w, h int) {
	if m.nvim == nil {
		return
	}
	if !m.resizeT.request(w, h, m.dragKind != dragNone, time.Now()) {
		m.resizeT.panel = true
		return
	}
	m.doNvimResize(w, h)
	m.resizeTerminalSplit()
}

func (m *Model) doNvimResize(w, h int) {
	_ = m.nvim.Resize(w, h)
}

// resizeAfterUpdate runs after every Update: when a drag ended, the last
// size goes out at once; while dragging, one flush tick is kept queued.
func (m *Model) resizeAfterUpdate() tea.Cmd {
	if !m.resizeT.pending {
		return nil
	}
	if m.dragKind == dragNone {
		if w, h, ok := m.resizeT.flush(time.Now()); ok && m.nvim != nil {
			m.doNvimResize(w, h)
			m.resizeTerminalSplit()
		}
		return nil
	}
	if m.resizeT.scheduled {
		return nil
	}
	m.resizeT.scheduled = true
	return tea.Tick(resizeInterval, func(time.Time) tea.Msg { return resizeFlushMsg{} })
}

// onResizeFlush sends the pending size (tick from resizeAfterUpdate).
func (m *Model) onResizeFlush() {
	if w, h, ok := m.resizeT.flush(time.Now()); ok && m.nvim != nil {
		m.doNvimResize(w, h)
		m.resizeTerminalSplit()
	}
}
