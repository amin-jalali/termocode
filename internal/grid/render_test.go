package grid

import (
	"strings"
	"testing"
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
