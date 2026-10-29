package grid

import (
	"math/rand"
	"testing"

	"github.com/muesli/termenv"
)

// A change to one line draws only that line again (and the rows the
// cursor left and entered), not the whole screen.
func TestRenderRedrawsOnlyChangedRows(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(2)), 80, 30, 10, false)
	g.CursorRow, g.CursorCol = 5, 3
	_ = g.Render()
	n := g.rowsDrawn
	g.Apply([][]any{{"grid_line", []any{1, 12, 0, []any{[]any{"x", 2}}}}})
	assertSameAsLegacy(t, g, "one line")
	if d := g.rowsDrawn - n; d != 1 {
		t.Fatalf("one changed line drew %d rows, want 1", d)
	}
	n = g.rowsDrawn
	g.Apply([][]any{{"grid_cursor_goto", []any{1, 7, 1}}})
	assertSameAsLegacy(t, g, "cursor move")
	if d := g.rowsDrawn - n; d != 2 {
		t.Fatalf("a cursor move drew %d rows, want 2 (the old and the new cursor row)", d)
	}
	n = g.rowsDrawn
	// A full-width scroll moves the drawn rows; only the rows nvim then
	// draws (and the cursor row) are drawn again.
	g.Apply([][]any{
		{"grid_scroll", []any{1, 0, 30, 0, 80, 1, 0}},
		{"grid_line", []any{1, 29, 0, []any{[]any{"new", 1}}}},
	})
	assertSameAsLegacy(t, g, "scroll")
	if d := g.rowsDrawn - n; d > 3 {
		t.Fatalf("a one-row scroll drew %d rows, want at most 3", d)
	}
}

// Random redraw events, each frame checked against the reference
// renderer: the row cache must never show a stale row.
func TestRowCacheMatchesLegacyUnderRandomEvents(t *testing.T) {
	for _, p := range []termenv.Profile{termenv.TrueColor, termenv.ANSI} {
		withProfile(t, p)
		rng := rand.New(rand.NewSource(int64(p) + 11))
		for round := 0; round < 20; round++ {
			w, h := 5+rng.Intn(40), 2+rng.Intn(12)
			g := randomGrid(rng, w, h, 8, false)
			assertSameAsLegacy(t, g, "start")
			for step := 0; step < 60; step++ {
				switch rng.Intn(9) {
				case 0, 1:
					cells := []any{}
					for k := rng.Intn(6); k >= 0; k-- {
						cells = append(cells, []any{string(rune('a' + rng.Intn(26))), rng.Intn(10), 1 + rng.Intn(3)})
					}
					g.Apply([][]any{{"grid_line", []any{1, rng.Intn(h), rng.Intn(w), cells}}})
				case 2:
					g.Apply([][]any{{"grid_cursor_goto", []any{1, rng.Intn(h), rng.Intn(w)}}})
				case 3:
					top := rng.Intn(h)
					bot := top + 1 + rng.Intn(h-top)
					left, right := 0, w
					if rng.Intn(2) == 0 {
						left = rng.Intn(w)
						right = left + 1 + rng.Intn(w-left)
					}
					rows := rng.Intn(3) + 1
					if rng.Intn(2) == 0 {
						rows = -rows
					}
					g.Apply([][]any{{"grid_scroll", []any{1, top, bot, left, right, rows, 0}}})
				case 4:
					g.Apply([][]any{{"hl_attr_define", []any{rng.Intn(10), map[string]any{"foreground": rng.Intn(0x1000000), "bold": rng.Intn(2) == 0}, map[string]any{}, []any{}}}})
				case 5:
					if rng.Intn(4) == 0 {
						g.Apply([][]any{{"default_colors_set", []any{rng.Intn(0x1000000), rng.Intn(0x1000000), -1, 0, 0}}})
					} else {
						g.Apply([][]any{{"grid_clear", []any{1}}})
					}
				case 6:
					g.CursorRow, g.CursorCol = rng.Intn(h+1)-1, rng.Intn(w+1)
				case 7:
					g.Cells[rng.Intn(h)][rng.Intn(w)] = Cell{Rune: 'Q', HlID: rng.Intn(10)}
					g.Invalidate()
				case 8:
					g.SetTermPalette([16]int{3: rng.Intn(0x1000000)})
				}
				assertSameAsLegacy(t, g, "step")
			}
		}
	}
}

// RenderLines is Render split into rows, and the caller may change the
// slice it gets.
func TestRenderLinesMatchesRender(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(4)), 30, 5, 6, false)
	lines := g.RenderLines()
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	joined := ""
	for i, l := range lines {
		if i > 0 {
			joined += "\n"
		}
		joined += l
	}
	if joined != g.Render() {
		t.Fatal("RenderLines differs from Render")
	}
	lines[0] = "changed"
	if g.RenderLines()[0] == "changed" {
		t.Fatal("RenderLines hands out the grid's own rows")
	}
}

// BenchmarkGridRenderTyping is one typed letter: one line drawn again and
// the cursor one cell on.
func BenchmarkGridRenderTyping(b *testing.B) {
	withProfile(b, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(1)), 200, 50, 40, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Apply([][]any{
			{"grid_line", []any{1, 10, 4 + i%100, []any{[]any{"x", 3}}}},
			{"grid_cursor_goto", []any{1, 10, 5 + i%100}},
		})
		_ = g.Render()
	}
}
