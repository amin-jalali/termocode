package grid

import (
	"fmt"
	"testing"
)

// Spec (WEB-40): RowRuns reads a row's text and the runs drawn with a
// style other than the default, with UTF-16 columns of that text: a wide
// glyph's second cell adds nothing, an emoji counts two units; a
// terminal palette colour follows the theme's palette; reverse swaps.
func TestRowRuns(t *testing.T) {
	g := New()
	g.Apply([][]any{
		{"grid_resize", []any{1, 12, 1}},
		{"default_colors_set", []any{0xdddddd, 0x111111, -1}},
		{"hl_attr_define",
			[]any{3, map[string]any{"foreground": 0xff0000, "bold": true}, map[string]any{}, []any{}},
			[]any{4, map[string]any{"reverse": true}, map[string]any{}, []any{}},
			[]any{5, map[string]any{"foreground": 0x00ff00}, map[string]any{"foreground": 2}, []any{map[string]any{"kind": "term"}}},
		},
		{"grid_line", []any{1, 0, 0, []any{
			[]any{"a", 0}, []any{"界", 3}, []any{"", 3}, []any{"😀", 3}, []any{"", 3},
			[]any{"x", 4}, []any{"y", 5}, []any{" ", 0, 5},
		}}},
	})
	g.SetTermPalette([16]int{0, 0, 0x123456})
	text, runs := g.RowRuns(0, 0, 12)
	if text != "a界😀xy     " {
		t.Fatalf("text %q", text)
	}
	got := fmt.Sprint(runs)
	want := "[{1 4 16711680 -1 true false false false} {4 5 1118481 14540253 false false false false} {5 6 1193046 -1 false false false false}]"
	if got != want {
		t.Errorf("runs\n got %s\nwant %s", got, want)
	}
	// A window of the row: columns [5, 7) are "xy".
	text, runs = g.RowRuns(0, 5, 7)
	if text != "xy" || len(runs) != 2 || runs[0].From != 0 || runs[1].From != 1 {
		t.Errorf("window: %q %v", text, runs)
	}
	if text, runs = g.RowRuns(3, 0, 5); text != "" || runs != nil {
		t.Errorf("a row outside: %q %v", text, runs)
	}
}
