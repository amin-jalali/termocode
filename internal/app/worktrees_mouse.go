package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
)

// ── WORKTREES view: mouse ────────────────────────────────────────────────

// wtRowAt is the row index on sidebar row y (-1 = none) in the list as
// it is drawn now.
func (m Model) wtRowAt(y int) int {
	st := m.wt
	if st == nil || !st.loaded || st.err != "" || st.detail && wtCompact(m.explorerWidth-1, m.h) {
		return -1
	}
	items, rows := m.wtList()
	_, own := st.bodyLines(items, rows, st.cursorIndex(items, rows), m.explorerWidth-1, m.h, true, clock.Now())
	i := y - wtListTop
	if i < 0 || i >= len(own) {
		return -1
	}
	return own[i]
}

// handleWTSidebarMouse: the wheel moves, a click selects a card, a second
// click on the selected card does what Enter does.
func (m Model) handleWTSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	st := m.wt
	switch t {
	case tea.MouseWheelUp:
		return m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyUp})
	case tea.MouseWheelDown:
		return m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyDown})
	case tea.MouseLeft:
	default:
		return m, nil
	}
	m.focus = FocusExplorer
	idx := m.wtRowAt(y)
	if idx < 0 {
		return m, nil
	}
	items, rows := m.wtList()
	if rows[idx].item < 0 {
		return m, nil
	}
	path := items[rows[idx].item].e.Path
	again := path == st.cursorPath && time.Since(st.lastClick) < 400*time.Millisecond
	st.cursorPath, st.lastClick = path, time.Now()
	st.touch()
	if again {
		return m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return m, nil
}

// wtHits are the click targets of the view (sidebar-local x): one per
// drawn line of a card.
func (m Model) wtHits(w int) []hitTarget {
	st := m.wt
	if st == nil || !st.loaded || st.err != "" {
		return nil
	}
	items, rows := m.wtList()
	_, own := st.bodyLines(items, rows, st.cursorIndex(items, rows), w, m.h, true, clock.Now())
	var out []hitTarget
	for i, idx := range own {
		if idx < 0 || rows[idx].item < 0 {
			continue
		}
		it := items[rows[idx].item]
		out = append(out, hitTarget{ID: "wt:" + it.e.Path, Label: wtTitle(it), X: 0, Y: wtListTop + i, W: w, H: 1})
	}
	return out
}
