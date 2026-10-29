package grid

import "testing"

// A grid_line that starts left of column 0 (a malformed redraw) must not
// crash the app: cells left of the grid are dropped, the rest are drawn.
func TestGridLineStartingLeftOfTheGridDoesNotCrash(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 4, 1}}})
	g.Apply([][]any{{"grid_line", []any{1, 0, -2, []any{[]any{"a", 0}, []any{"b"}, []any{"c"}, []any{"d"}}}}})
	if got := string([]rune{g.Cells[0][0].Rune, g.Cells[0][1].Rune}); got != "cd" {
		t.Errorf("row starts %q, want cd", got)
	}
}
