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

// resizeThrottle defers nvim resize requests during a drag.
type resizeThrottle struct {
	lastReq   time.Time // last size request while dragging
	pending   bool      // a size waits to be sent
	scheduled bool      // a flush tick is queued
	w, h      int       // latest requested size
}

// request records a new size. It returns true when the caller should
// resize now (no drag in progress); during a drag the size is kept for
// the release / idle flush.
func (t *resizeThrottle) request(w, h int, dragging bool, now time.Time) bool {
	t.w, t.h = w, h
	if !dragging {
		t.pending = false
		return true
	}
	t.pending = true
	t.lastReq = now
	return false
}

// flush returns the pending size when it should go out now: always after
// the drag ended, else only once the pointer rested for dragIdleResize.
func (t *resizeThrottle) flush(dragging bool, now time.Time) (w, h int, ok bool) {
	t.scheduled = false
	if !t.pending {
		return 0, 0, false
	}
	if dragging && now.Sub(t.lastReq) < dragIdleResize {
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
	if !m.resizeT.request(w, h, m.dragKind != dragNone, time.Now()) {
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
	if m.dragKind == dragNone {
		m.onResizeFlush()
		return nil
	}
	if m.resizeT.scheduled {
		return nil
	}
	m.resizeT.scheduled = true
	return tea.Tick(dragIdleResize, func(time.Time) tea.Msg { return resizeFlushMsg{} })
}

// onResizeFlush sends the pending size if it is due.
func (m *Model) onResizeFlush() {
	if w, h, ok := m.resizeT.flush(m.dragKind != dragNone, time.Now()); ok && m.nvim != nil {
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
