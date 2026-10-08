package statusbar

import "github.com/charmbracelet/lipgloss"

// debugBadgeText is the right-most part of the right cluster while a
// debug session is live (see renderRightChip).
const debugBadgeText = "● DEBUG"

// DebugBadgeSpan returns the [x0, x1) cell range of the "● DEBUG" badge
// relative to the bar's left edge (Group D: a click opens the Run view);
// ok=false when no session is live or the badge was truncated away.
func (m Model) DebugBadgeSpan(s State) (x0, x1 int, ok bool) {
	if m.w <= 0 || !s.Debug {
		return 0, 0, false
	}
	l := m.layout(s)
	full := lipgloss.Width(m.renderRight(s))
	if l.rightW < full {
		return 0, 0, false
	}
	rightX := 1 + l.leftW + l.gapL + l.centerW + l.gapR
	x1 = rightX + l.rightW
	return x1 - lipgloss.Width(debugBadgeText), x1, true
}
