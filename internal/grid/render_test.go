package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderProducesText(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 5, 1}}})
	g.Apply([][]any{{"default_colors_set", []any{0xd4d4d4, 0x1e1e1e, 0, 0, 0}}})
	g.Apply([][]any{{"grid_line",
		[]any{1, 0, 0, []any{
			[]any{"h", 0},
			[]any{"e", 0},
			[]any{"l", 0},
			[]any{"l", 0},
			[]any{"o", 0},
		}},
	}})

	out := g.Render()
	if !strings.Contains(out, "h") || !strings.Contains(out, "o") {
		t.Fatalf("render missing chars: %q", out)
	}
}

func TestRenderEmptyGrid(t *testing.T) {
	g := New()
	if got := g.Render(); got != "" {
		t.Fatalf("empty grid: got %q", got)
	}
}

// A double-width glyph takes two grid cells; nvim sends the right half as
// "". Rendering that half as a space shifted the rest of the row one
// column right (found by the e2e suite: "界 world" showed as "界  world").
func TestRenderWideGlyphKeepsRowWidth(t *testing.T) {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 6, 1}}})
	g.Apply([][]any{{"grid_line",
		[]any{1, 0, 0, []any{
			[]any{"a", 0},
			[]any{"界", 0},
			[]any{"", 0},
			[]any{"b", 0},
			[]any{" ", 0, 2},
		}},
	}})
	for _, cursor := range []int{0, 1, 3} {
		g.CursorRow, g.CursorCol = 0, cursor
		out := g.Render()
		if got := ansi.Strip(out); got != "a界b  " {
			t.Fatalf("cursor %d: got %q, want %q", cursor, got, "a界b  ")
		}
		if w := ansi.StringWidth(out); w != 6 {
			t.Fatalf("cursor %d: width %d, want 6", cursor, w)
		}
	}
}
