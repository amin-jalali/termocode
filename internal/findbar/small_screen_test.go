package findbar

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSmallScreenWidth(t *testing.T) {
	for w := 12; w <= 40; w++ {
		m := New()
		m.SetSize(w, 30)
		m.SetQuery(strings.Repeat("query", 20))
		m.EnableReplace(true)
		view := m.View()
		for _, line := range strings.Split(view, "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Fatalf("screen %d: row width %d: %q", w, got, ansi.Strip(line))
			}
		}
	}
}

func TestSmallScreenButtonsMatchGlyphs(t *testing.T) {
	for _, w := range []int{12, 24, 38, 80} {
		m := New()
		m.SetSize(w, 30)
		m.EnableReplace(true)
		rows := strings.Split(ansi.Strip(m.View()), "\n")
		glyphs := map[string]string{"chevron": "▾", "case": "Aa", "word": "ab", "regex": ".*", "prev": "↑", "next": "↓", "close": "×", "replace": "↵", "replace-all": "⇒"}
		for _, b := range m.Buttons() {
			if got := ansi.Cut(rows[b.Y], b.X, b.X+b.W); got != glyphs[b.ID] {
				t.Errorf("width %d: %s at (%d,%d) got %q", w, b.ID, b.X, b.Y, got)
			}
		}
	}
}
