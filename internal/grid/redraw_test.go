package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// rowText is row r of g as plain text.
func rowText(g *Grid, r int) string {
	var b strings.Builder
	for _, c := range g.Cells[r] {
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// filled is a w×h grid whose row r is the letter 'a'+r repeated.
func filled(w, h int) *Grid {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, w, h}}})
	for r := 0; r < h; r++ {
		g.Apply([][]any{{"grid_line", []any{1, r, 0, []any{[]any{string(rune('a' + r)), 0, w}}}}})
	}
	return g
}

func TestNumbersOfEveryMsgpackTypeAreRead(t *testing.T) {
	for _, v := range []any{int8(7), int16(7), int32(7), int64(7), uint(7), uint8(7), uint16(7), uint32(7), uint64(7), float32(7), float64(7.9)} {
		g := New()
		g.Apply([][]any{{"grid_resize", []any{1, v, v}}})
		if g.Width != 7 || g.Height != 7 {
			t.Errorf("%T: grid %dx%d, want 7x7", v, g.Width, g.Height)
		}
	}
	if toInt("7") != 0 || toInt(nil) != 0 {
		t.Error("non-numbers should read as 0")
	}
}

func TestScrollDownMovesRowsDown(t *testing.T) {
	g := filled(3, 4) // aaa bbb ccc ddd
	g.Apply([][]any{{"grid_scroll", []any{1, 0, 4, 0, 3, -1, 0}}})
	want := []string{"aaa", "aaa", "bbb", "ccc"}
	for r, w := range want {
		if got := rowText(g, r); got != w {
			t.Errorf("row %d = %q, want %q", r, got, w)
		}
	}
}

func TestBadScrollsChangeNothing(t *testing.T) {
	for name, args := range map[string][]any{
		"zero rows":       {1, 0, 4, 0, 3, 0, 0},
		"top below bot":   {1, 3, 1, 0, 3, 1, 0},
		"bot past grid":   {1, 0, 9, 0, 3, 1, 0},
		"negative left":   {1, 0, 4, -1, 3, 1, 0},
		"right too far":   {1, 0, 4, 0, 9, 1, 0},
		"left past right": {1, 0, 4, 2, 1, 1, 0},
		"too few args":    {1, 0, 4},
	} {
		g := filled(3, 4)
		g.Apply([][]any{{"grid_scroll", args}})
		for r, w := range []string{"aaa", "bbb", "ccc", "ddd"} {
			if got := rowText(g, r); got != w {
				t.Errorf("%s: row %d = %q, want %q", name, r, got, w)
			}
		}
	}
}

func TestShortOrOddEventsAreIgnored(t *testing.T) {
	g := filled(3, 2)
	g.Apply([][]any{
		{},                            // empty event
		{"flush"},                     // event with no args
		{"grid_resize", []any{1, 9}},  // too few args
		{"grid_line", []any{1, 0, 0}}, // too few args
		{"grid_line", []any{1, 5, 0, []any{[]any{"z"}}}}, // row off the grid
		{"grid_line", []any{1, 0, 0, []any{[]any{}}}},    // empty cell
		{"grid_cursor_goto", []any{1, 1}},                // too few args
		{"default_colors_set", []any{1, 2}},              // too few args
		{"hl_attr_define", []any{1}},                     // too few args
		{"hl_attr_define", []any{2, "not a map"}},
	})
	if g.Width != 3 || g.Height != 2 || rowText(g, 0) != "aaa" || rowText(g, 1) != "bbb" {
		t.Errorf("odd events changed the grid: %dx%d %q %q", g.Width, g.Height, rowText(g, 0), rowText(g, 1))
	}
	if g.DefaultFg != -1 {
		t.Errorf("short default_colors_set applied: fg %d", g.DefaultFg)
	}
	if a := g.HlAttrs[2]; a.Fg != -1 || a.Bold {
		t.Errorf("hl 2 from a non-map = %+v, want plain defaults", a)
	}
	g.Apply([][]any{{"grid_cursor_goto", []any{1, 1, 2}}})
	if g.CursorRow != 1 || g.CursorCol != 2 {
		t.Errorf("cursor %d,%d", g.CursorRow, g.CursorCol)
	}
}

func TestDefaultColorsAndAttrsFromAnyKeyedMaps(t *testing.T) {
	g := New()
	g.Apply([][]any{{"default_colors_set", []any{0xffffff, 0x000000, 0xff0000}}})
	if g.DefaultFg != 0xffffff || g.DefaultBg != 0 || g.DefaultSp != 0xff0000 {
		t.Errorf("defaults %x %x %x", g.DefaultFg, g.DefaultBg, g.DefaultSp)
	}
	g.Apply([][]any{{"hl_attr_define", []any{3, map[any]any{
		"foreground": int64(0x112233), "background": uint32(0x445566), "special": 0x778899,
		"bold": true, "italic": true, "underline": true, "undercurl": true,
		"strikethrough": true, "reverse": true, 42: "ignored non-string key",
	}}}})
	a := g.HlAttrs[3]
	if a.Fg != 0x112233 || a.Bg != 0x445566 || a.Sp != 0x778899 {
		t.Errorf("colours %x %x %x", a.Fg, a.Bg, a.Sp)
	}
	if !a.Bold || !a.Italic || !a.Underline || !a.Undercurl || !a.Strikethrough || !a.Reverse {
		t.Errorf("flags lost: %+v", a)
	}
}

func TestTerminalColoursOnlyForPureTerminalCells(t *testing.T) {
	cterm := map[string]any{"foreground": 2, "background": 4}
	cases := []struct {
		name string
		info any
		term bool
	}{
		{"term (any-keyed)", []any{map[any]any{"kind": "term"}}, true},
		{"term (string-keyed)", []any{map[string]any{"kind": "term"}}, true},
		{"term + ui group", []any{map[string]any{"kind": "term"}, map[string]any{"kind": "ui"}}, false},
		{"empty info", []any{}, false},
		{"not a list", "term", false},
		{"odd item", []any{"term"}, false},
	}
	for _, c := range cases {
		g := New()
		g.Apply([][]any{{"hl_attr_define", []any{5, map[string]any{"foreground": 0x00ff00}, cterm, c.info}}})
		a := g.HlAttrs[5]
		if got := a.TermFg == 3 && a.TermBg == 5; got != c.term {
			t.Errorf("%s: TermFg %d TermBg %d, want terminal colours %v", c.name, a.TermFg, a.TermBg, c.term)
		}
	}
	// cterm colours outside 0–15 stay RGB.
	g := New()
	g.Apply([][]any{{"hl_attr_define", []any{6, map[string]any{}, map[string]any{"foreground": 200}, []any{map[string]any{"kind": "term"}}}}})
	if g.HlAttrs[6].TermFg != 0 {
		t.Errorf("cterm 200 mapped to palette slot %d", g.HlAttrs[6].TermFg)
	}
}

func TestInvalidRuneDrawsAReplacementChar(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 3, 1}}})
	g.Cells[0][1] = Cell{Rune: 0x110000}
	g.Cells[0][2] = Cell{Rune: 0}
	g.CursorRow, g.CursorCol = -1, -1
	out := ansi.Strip(g.Render())
	if out != " � " {
		t.Errorf("render = %q, want space, U+FFFD, space", out)
	}
}
