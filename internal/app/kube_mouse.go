package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Kubernetes view: mouse ───────────────────────────────────────────────

// kubeRowAt is the row index on sidebar row y (-1 = none).
func (st *kubeState) kubeRowAt(y, h int) int {
	i := y - kubeListTop
	lines := kubeBodyLines(st.rows(), st.cursor, kubeBodyH(h))
	if i < 0 || i >= len(lines) {
		return -1
	}
	return lines[i]
}

// handleKubeSidebarMouse: the wheel moves, a click selects a row (or
// folds a section), a second click on the selected row opens it as Enter
// does.
func (m Model) handleKubeSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	st := m.kube
	switch t {
	case tea.MouseWheelUp:
		st.move(-3)
		return m, nil
	case tea.MouseWheelDown:
		st.move(3)
		return m, nil
	case tea.MouseLeft:
	default:
		return m, nil
	}
	m.focus = FocusExplorer
	idx := st.kubeRowAt(y, m.h)
	if idx < 0 {
		return m, nil
	}
	r := st.rows()[idx]
	if !r.selectable() {
		return m, nil
	}
	again := idx == st.cursor && time.Since(st.lastClick) < 400*time.Millisecond
	st.cursor, st.lastClick = idx, time.Now()
	st.touch()
	if r.kind == kubeRowSection {
		st.toggleFold(r.section)
		return m, nil
	}
	if again {
		return m.handleKubeSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return m, nil
}

// kubeHits are the click targets of the view (sidebar-local x).
func (m Model) kubeHits(w int) []hitTarget {
	st := m.kube
	if st == nil || !st.loaded {
		return nil
	}
	var out []hitTarget
	rows := st.rows()
	for i, idx := range kubeBodyLines(rows, st.cursor, kubeBodyH(m.h)) {
		if idx < 0 {
			continue
		}
		switch r := rows[idx]; r.kind {
		case kubeRowSection:
			out = append(out, hitTarget{ID: "k8s:sec:" + r.section, Label: kubeSectionTitle(r.section), X: 0, Y: kubeListTop + i, W: w, H: 1})
		case kubeRowItem:
			out = append(out, hitTarget{ID: "k8s:row:" + r.section + "/" + r.name, Label: r.name, X: 0, Y: kubeListTop + i, W: w, H: 1})
		}
	}
	return out
}
