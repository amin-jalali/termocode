package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func fullState() State {
	return State{
		Path: "/work/termocode/main.go", Project: "termocode", Lang: "Go",
		Line: 123, Col: 45, Branch: "main", Errors: 3, Warnings: 2,
		Indent: "Spaces: 4", Encoding: "UTF-8",
		ShowLSP: true, LSP: []string{"gopls"},
		AI: "claude-sonnet", AIState: AIIdle,
	}
}

// Spec: Given a status bar with every item on (indent, encoding, language,
// LSP, AI, cursor position)
// When the bar is narrow (like the editor column of a 100-column window)
// Then low-value items are dropped whole, the "Ln, Col" pill always shows
// whole, and no item is cut with "…" (a tiny bar may shorten the
// project name).
func TestNarrowBarKeepsCursorPillWhole(t *testing.T) {
	m := New(theme.Theme{})
	s := fullState()
	pos := "Ln 123, Col 45"
	for w := 30; w <= 160; w++ {
		m.SetWidth(w)
		out := ansi.Strip(m.View(s))
		if got := lipgloss.Width(out); got != w {
			t.Fatalf("width %d: row is %d cells", w, got)
		}
		if !strings.Contains(out, pos) {
			t.Fatalf("width %d: cursor pill not whole: %q", w, out)
		}
		// Only a very small bar may shorten the project name.
		if w >= 40 && strings.Contains(out, "…") {
			t.Fatalf("width %d: an item is cut: %q", w, out)
		}
	}
}

// Spec: Given a bar with not enough room for everything
// When items must go
// Then the AI chip goes first, then LSP, encoding, indent, while the
// language and the branch stay.
func TestNarrowBarDropOrder(t *testing.T) {
	m := New(theme.Theme{})
	s := fullState()
	m.SetWidth(80)
	out := ansi.Strip(m.View(s))
	for _, gone := range []string{"claude-sonnet", "gopls"} {
		if strings.Contains(out, gone) {
			t.Errorf("width 80: %q should be dropped first: %q", gone, out)
		}
	}
	for _, keep := range []string{"termocode", "main", "Go", "Ln 123, Col 45"} {
		if !strings.Contains(out, keep) {
			t.Errorf("width 80: %q should stay: %q", keep, out)
		}
	}
	if _, _, ok := m.AIBadgeSpan(s); ok {
		t.Error("dropped AI badge must not be clickable")
	}
}
