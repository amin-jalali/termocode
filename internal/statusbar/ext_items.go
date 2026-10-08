package statusbar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"termocode/internal/ext"
	"termocode/internal/theme"
)

// ── Extension status items (Group I) ─────────────────────────────────────

// centerGap is the separator renderCenter puts between its parts.
const centerGap = 2

// renderExtParts renders the non-empty extension items with the bar's
// background. Items are markup ("{{warning}}3{{/}} todos"), never ANSI.
func renderExtParts(items []string) []string {
	var out []string
	for _, it := range items {
		if strings.TrimSpace(ext.PlainText(it)) == "" {
			continue
		}
		out = append(out, ext.Render(it, theme.TextSecondary, theme.LG(barBgColor())))
	}
	return out
}

// ExtItemSpan is the clickable cell range [X0, X1) of status item Index
// (an index into State.Ext), relative to the bar's left edge.
type ExtItemSpan struct {
	Index  int
	X0, X1 int
}

// ExtItemSpans returns where each visible extension item sits on the bar.
// Empty when the center cluster was dropped for lack of room.
func (m Model) ExtItemSpans(s State) []ExtItemSpan {
	if m.w <= 0 || len(s.Ext) == 0 {
		return nil
	}
	l := m.layout(s)
	if l.centerW == 0 {
		return nil
	}
	base := s
	base.Ext = nil
	baseW := lipgloss.Width(m.renderCenter(base))
	x := 1 + l.leftW + l.gapL + baseW
	if baseW > 0 {
		x += centerGap
	}
	var out []ExtItemSpan
	for i, it := range s.Ext {
		if strings.TrimSpace(ext.PlainText(it)) == "" {
			continue
		}
		w := lipgloss.Width(ext.PlainText(it))
		out = append(out, ExtItemSpan{Index: i, X0: x, X1: x + w})
		x += w + centerGap
	}
	return out
}
