package editor

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The grid reuses its last frame while nothing changes; every redraw and
// a theme's terminal palette must still show up in the next View.
func TestViewFollowsRedrawAndPalette(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	m := New(nil)
	m.SetSize(10, 2)
	m.ApplyRedraw([][]any{
		{"grid_resize", []any{1, 10, 2}},
		{"hl_attr_define", []any{1, map[string]any{"foreground": 0x010101}, map[string]any{"foreground": 2},
			[]any{map[string]any{"kind": "term"}}}},
		{"grid_line", []any{1, 0, 0, []any{[]any{"ab", 1}}}},
	})
	v1 := m.View(true)
	if v := m.View(false); v != v1 {
		t.Fatalf("unchanged grid gave a new frame:\n%q\n%q", v1, v)
	}

	m.ApplyRedraw([][]any{{"grid_line", []any{1, 1, 0, []any{[]any{"x", 0}}}}})
	v2 := m.View(true)
	if v2 == v1 {
		t.Fatal("grid_line not shown: stale frame")
	}

	m.SetTermPalette([16]int{2: 0xff0000})
	v3 := m.View(true)
	if v3 == v2 {
		t.Fatal("terminal palette not shown: stale frame")
	}
}
