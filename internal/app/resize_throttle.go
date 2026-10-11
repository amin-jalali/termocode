package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Splitter drags (explorer, AI / actions panel, bottom panel).
//
// Resizing nvim is the expensive part of a drag: nvim_ui_try_resize is a
// synchronous RPC and a narrower grid makes nvim re-layout and send a full
// redraw, which queues up behind the pointer. So while a drag is active
// nvim is NOT resized on every step: the chrome follows every motion
// event, the last nvim grid is painted clipped (or padded with the editor
// background) into the new editor rect, and nvim is resized once the
// drag ends — or after the pointer has rested for dragIdleResize.

const dragIdleResize = 250 * time.Millisecond

// winIdleResize is the same for terminal window resizes: dragging the
// window's edge sends a burst of size events, and waiting on nvim for each
// one made the whole resize stutter. nvim gets the size once the burst has
// been quiet this long.
const winIdleResize = 60 * time.Millisecond

// resizeThrottle defers nvim resize requests during a drag.
type resizeThrottle struct {
	lastReq   time.Time // last size request while dragging
	pending   bool      // a size waits to be sent
	scheduled bool      // a flush tick is queued
	w, h      int       // latest requested size
}

// request records a new size. It returns true when the caller should
// resize now (no drag or window resize in progress); otherwise the size is
// kept for the release / idle flush.
func (t *resizeThrottle) request(w, h int, deferring bool, now time.Time) bool {
	t.w, t.h = w, h
	if !deferring {
		t.pending = false
		return true
	}
	t.pending = true
	t.lastReq = now
	return false
}

// flush returns the pending size when it should go out now: at once when
// idle is 0 (the drag ended), else only once no new size came for idle.
func (t *resizeThrottle) flush(idle time.Duration, now time.Time) (w, h int, ok bool) {
	t.scheduled = false
	if !t.pending {
		return 0, 0, false
	}
	if now.Sub(t.lastReq) < idle {
		return 0, 0, false
	}
	t.pending = false
	return t.w, t.h, true
}

type resizeFlushMsg struct{}

// sendNvimResize applies the editor size to nvim, deferred while a
// splitter drag is in progress.
func (m *Model) sendNvimResize(w, h int) {
	if m.nvim == nil {
		return
	}
	if !m.resizeT.request(w, h, m.resizeIdle(time.Now()) > 0, time.Now()) {
		return
	}
	_ = m.nvim.Resize(w, h)
	m.reapplyTerminalSplit()
}

// resizeAfterUpdate runs after every Update: when a drag ended, the last
// size goes out at once; while dragging, one idle-check tick is queued.
func (m *Model) resizeAfterUpdate() tea.Cmd {
	if !m.resizeT.pending {
		return nil
	}
	idle := m.resizeIdle(time.Now())
	if idle == 0 {
		m.onResizeFlush()
		return nil
	}
	if m.resizeT.scheduled {
		return nil
	}
	m.resizeT.scheduled = true
	return tea.Tick(idle, func(time.Time) tea.Msg { return resizeFlushMsg{} })
}

// resizeIdle is how long nvim resizes are held back right now: the drag
// idle time during a splitter drag, the window idle time during a burst of
// window size events, else 0 (resize at once).
func (m *Model) resizeIdle(now time.Time) time.Duration {
	switch {
	case m.dragKind != dragNone:
		return dragIdleResize
	case now.Sub(m.lastWinResize) < winIdleResize:
		return winIdleResize
	}
	return 0
}

// onResizeFlush sends the pending size if it is due.
func (m *Model) onResizeFlush() {
	if w, h, ok := m.resizeT.flush(m.resizeIdle(time.Now()), time.Now()); ok && m.nvim != nil {
		_ = m.nvim.Resize(w, h)
		m.reapplyTerminalSplit()
	}
}

// reapplyTerminalSplit sets the panel height again after nvim's grid was
// resized. nvim squeezes the panel window when the grid gets too small and
// does not give the rows back when it grows, so the cached "already sent"
// height is not valid any more. Skipping the call left the shell drawn
// above the panel's tab bar after a small-then-large resize (found by the
// e2e suite).
func (m *Model) reapplyTerminalSplit() {
	m.terminalRowsLastSent = -1
	m.resizeTerminalSplit()
}
