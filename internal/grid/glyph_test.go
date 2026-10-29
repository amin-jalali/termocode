package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A glyph of several runes (a letter with a combining accent, an emoji
// ZWJ sequence, a flag) that nvim sends in one cell is drawn whole, and
// the row keeps the grid width.
func TestGridDrawsGlyphsOfSeveralRunes(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 10, 1}}})
	g.Apply([][]any{{"grid_line", []any{1, 0, 0, []any{
		[]any{"é", 0},
		[]any{"👩‍💻"}, []any{""},
		[]any{"🇮🇷"}, []any{""},
		[]any{"x"},
	}}}})
	g.CursorRow, g.CursorCol = -1, -1
	row := ansi.Strip(strings.Split(g.Render(), "\n")[0])
	if want := "é👩‍💻🇮🇷x"; !strings.HasPrefix(row, want) {
		t.Errorf("row = %q, want it to start with %q", row, want)
	}
	if w := ansi.StringWidth(row); w != 10 {
		t.Errorf("row is %d cells, want 10: %q", w, row)
	}
}
