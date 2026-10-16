package app

import "strconv"

// statusSpan is a clickable chip of the status bar, in bar-local cells
// [x0, x1).
type statusSpan struct {
	id     string // "status:ai", "status:lsp", …, "status:ext:N"
	x0, x1 int
}

// statusSpans lists the clickable status-bar chips in the order a click
// tests them. The bar is drawn under the editor column (statusBarRect).
func (m Model) statusSpans() []statusSpan {
	if m.zenMode {
		return nil
	}
	_, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	st := m.statusState()
	var out []statusSpan
	add := func(id string, x0, x1 int, ok bool) {
		if ok && x1 > x0 {
			out = append(out, statusSpan{id, x0, x1})
		}
	}
	x0, x1, ok := sb.AIBadgeSpan(st)
	add("status:ai", x0, x1, ok)
	x0, x1, ok = sb.LSPChipSpan(st)
	add("status:lsp", x0, x1, ok)
	x0, x1, ok = sb.DiagSpan(st)
	add("status:diag", x0, x1, ok)
	x0, x1, ok = sb.TestsChipSpan(st)
	add("status:tests", x0, x1, ok)
	x0, x1, ok = sb.ServicesChipSpan(st)
	add("status:services", x0, x1, ok)
	if m.ext != nil && len(st.Ext) > 0 {
		for _, sp := range sb.ExtItemSpans(st) {
			add("status:ext:"+strconv.Itoa(sp.Index), sp.X0, sp.X1, true)
		}
	}
	if m.dapSessionActive {
		x0, x1, ok = sb.DebugBadgeSpan(st)
		add("status:debug", x0, x1, ok)
	}
	// Plain items: a click does nothing, but a hover label names them.
	for _, sp := range sb.ItemSpans(st) {
		add("status:"+sp.ID, sp.X0, sp.X1, true)
	}
	return out
}

// onStatusBar reports whether screen cell (x, y) is on the status bar:
// the last row, under the editor column (statusBarRect).
func (m Model) onStatusBar(x, y int) bool {
	if m.zenMode || y < m.h-1 {
		return false
	}
	bx, bw := m.statusBarRect()
	return x >= bx && x < bx+bw
}

// statusHitAt returns the id of the status-bar chip at screen cell
// (x, y), or "".
func (m Model) statusHitAt(x, y int) string {
	if m.zenMode || y != m.h-1 {
		return ""
	}
	bx, _ := m.statusBarRect()
	for _, s := range m.statusSpans() {
		if x >= bx+s.x0 && x < bx+s.x1 {
			return s.id
		}
	}
	return ""
}
