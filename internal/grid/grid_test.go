package grid

import "testing"

func TestResizeAndClear(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 5, 3}}})
	if g.Width != 5 || g.Height != 3 {
		t.Fatalf("size: %dx%d", g.Width, g.Height)
	}
	if len(g.Cells) != 3 || len(g.Cells[0]) != 5 {
		t.Fatalf("cells dims: %dx%d", len(g.Cells), len(g.Cells[0]))
	}
	for _, row := range g.Cells {
		for _, c := range row {
			if c.Rune != ' ' {
				t.Fatalf("cell not initialized to space: %q", c.Rune)
			}
		}
	}
}

func TestGridLineSetsCells(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 6, 2}}})
	g.Apply([][]any{{"grid_line",
		[]any{1, 0, 0, []any{
			[]any{"h", 5},
			[]any{"i", 5},
			[]any{" ", 0, 4}, // 4 spaces with hl=0
		}},
	}})
	got := string([]rune{
		g.Cells[0][0].Rune, g.Cells[0][1].Rune, g.Cells[0][2].Rune,
		g.Cells[0][3].Rune, g.Cells[0][4].Rune, g.Cells[0][5].Rune,
	})
	if got != "hi    " {
		t.Fatalf("got %q", got)
	}
	if g.Cells[0][0].HlID != 5 || g.Cells[0][1].HlID != 5 || g.Cells[0][5].HlID != 0 {
		t.Fatalf("hl ids: %v", g.Cells[0])
	}
}

func TestGridScrollUp(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 4, 4}}})
	// fill cells with row letter
	letters := []string{"A", "B", "C", "D"}
	for r, l := range letters {
		g.Apply([][]any{{"grid_line",
			[]any{1, r, 0, []any{[]any{l, 0, 4}}},
		}})
	}
	// scroll up by 1 row in full grid
	g.Apply([][]any{{"grid_scroll", []any{1, 0, 4, 0, 4, 1, 0}}})
	if g.Cells[0][0].Rune != 'B' || g.Cells[1][0].Rune != 'C' || g.Cells[2][0].Rune != 'D' {
		t.Fatalf("post-scroll cells: %c %c %c", g.Cells[0][0].Rune, g.Cells[1][0].Rune, g.Cells[2][0].Rune)
	}
}

func TestHlAttrDefine(t *testing.T) {
	g := New()
	g.Apply([][]any{{"hl_attr_define",
		[]any{42, map[string]any{
			"foreground": 0xff0000,
			"background": 0x00ff00,
			"bold":       true,
		}, map[string]any{}, []any{}},
	}})
	a, ok := g.HlAttrs[42]
	if !ok {
		t.Fatalf("hl 42 missing")
	}
	if a.Fg != 0xff0000 || a.Bg != 0x00ff00 || !a.Bold {
		t.Fatalf("attrs: %+v", a)
	}
}

func TestCursorGoto(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 10, 5}}})
	g.Apply([][]any{{"grid_cursor_goto", []any{1, 3, 7}}})
	if g.CursorRow != 3 || g.CursorCol != 7 {
		t.Fatalf("cursor: %d,%d", g.CursorRow, g.CursorCol)
	}
}
