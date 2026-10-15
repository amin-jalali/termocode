package app

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #15): the breadcrumb reads on every theme. Folders use
// the muted tier (not dim), and nothing uses SGR 2 (faint): many terminals
// ignore it or make it unreadable on light backgrounds.
func TestBreadcrumbReadableNoFaint(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	defer theme.ApplyTheme("vscode-dark-plus")
	for _, id := range []string{"vscode-dark-plus", "catppuccin-latte"} {
		theme.ApplyTheme(id)
		m := Model{breadcrumbs: []string{"internal", "app", "view.go"}, stickyContext: "func (m Model) View() string {"}
		cells := parseANSIRow(m.drawBreadcrumbs(120))
		for i, c := range cells {
			if parseSGR(c.sgr).faint {
				t.Fatalf("%s: cell %d is faint", id, i)
			}
		}
		// "internal" starts at cell 1.
		if got, want := parseSGR(cells[1].sgr).fg, theme.ToRGB(theme.TextMuted); got != want {
			t.Errorf("%s: folder colour %v, want TextMuted %v", id, got, want)
		}
	}
}
