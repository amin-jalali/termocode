package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// ── Kubernetes view: keys ────────────────────────────────────────────────

// handleKubeSidebarKey is a key while the Kubernetes view has the focus.
func (m Model) handleKubeSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.kube
	if text, typing, handled := inlineFilterStep(st.filter, st.filterTyping, k); handled {
		st.filterTyping = typing
		st.setFilter(text)
		return m, nil
	}
	r, have := st.currentRow()
	switch k.String() {
	case "up", "k":
		st.move(-1)
	case "down", "j":
		st.move(1)
	case "pgup":
		st.move(-10)
	case "pgdown":
		st.move(10)
	case "home", "g":
		st.cursor = 0
		st.touch()
	case "end", "G":
		st.cursor = len(st.rows()) - 1
		st.clampCursor()
		st.touch()
	case "tab":
		st.jumpSection(1)
	case "shift+tab":
		st.jumpSection(-1)
	case " ":
		if have && r.kind == kubeRowSection {
			st.toggleFold(r.section)
		}
	case "enter", "l":
		if have && r.kind == kubeRowSection {
			st.toggleFold(r.section)
		}
	case "R":
		return m, m.kubeReload()
	case "c":
		return m, m.kubePick("ctx")
	case "n":
		return m, m.kubePick("ns")
	case "esc":
		m.focus = FocusEditor
	}
	return m, nil
}

// move goes d selectable rows down (up when negative).
func (st *kubeState) move(d int) {
	rows := st.rows()
	if len(rows) == 0 {
		return
	}
	step := 1
	if d < 0 {
		step, d = -1, -d
	}
	cur := st.cursor
	for ; d > 0; d-- {
		next := cur + step
		for next >= 0 && next < len(rows) && !rows[next].selectable() {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		cur = next
	}
	st.cursor = cur
	st.touch()
}

// jumpSection moves to the next (1) or previous (-1) section header and
// wraps around at the ends.
func (st *kubeState) jumpSection(dir int) {
	rows := st.rows()
	n := len(rows)
	for i := 1; i <= n; i++ {
		j := ((st.cursor+dir*i)%n + n) % n
		if rows[j].kind == kubeRowSection {
			st.cursor = j
			st.touch()
			return
		}
	}
}

// toggleFold opens or folds a section; the cursor stays on its header.
func (st *kubeState) toggleFold(sec string) {
	st.collapsed[sec] = !st.collapsed[sec]
	for i, r := range st.rows() {
		if r.kind == kubeRowSection && r.section == sec {
			st.cursor = i
		}
	}
	st.touch()
}

// setFilter changes the filter and keeps the cursor on its object when it
// is still shown, else on the first match.
func (st *kubeState) setFilter(text string) {
	if st.filter == text {
		st.touch()
		return
	}
	cur, had := st.currentRow()
	st.filter = text
	st.touch()
	rows := st.rows()
	if had && cur.kind == kubeRowItem {
		for i, r := range rows {
			if r.kind == kubeRowItem && r.section == cur.section && r.name == cur.name {
				st.cursor = i
				return
			}
		}
	}
	for i, r := range rows {
		if r.kind == kubeRowItem {
			st.cursor = i
			return
		}
	}
	st.clampCursor()
}
