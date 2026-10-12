package app

import "github.com/amin-jalali/termocode/internal/activity"

// editorPane is where the editor pane sits on screen. The pane is the
// column between the sidebar (when shown) and the AI chat panel (when
// pinned open); its content area starts below the replace bar, the tab
// bar and the breadcrumbs row.
type editorPane struct {
	X, W int // first screen column and width (at least 1)
	Top  int // screen row of the first content row
	H    int // content rows (at least 1)
}

// editorPaneLayout is the one place that computes the editor pane's
// geometry. The view, applyLayout, the overlays and the mouse hit tests
// all use it, so what is drawn and what a click hits never disagree
// (e.g. the Welcome cards when the AI panel takes room on the right).
func (m Model) editorPaneLayout() editorPane {
	p := editorPane{X: activity.Width}
	p.W = m.w - activity.Width - editorScrollbarWidth - m.actionsColumnWidth()
	if m.showExp {
		p.X += m.explorerWidth
		p.W -= m.explorerWidth
	}
	if p.W < 1 {
		p.W = 1
	}
	if m.replaceOpen {
		p.Top += 2 // the replace bar is two rows
	}
	p.Top += m.tabs.Height()
	// Breadcrumbs and the sticky signature share one row.
	if len(m.breadcrumbs) > 0 || m.stickyContext != "" {
		p.Top++
	}
	bodyH := m.h - 1 // the status bar takes the last row
	if bodyH < 1 {
		bodyH = 1
	}
	p.H = bodyH - p.Top
	if p.H < 1 {
		p.H = 1
	}
	return p
}
