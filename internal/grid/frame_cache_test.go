package grid

import (
	"math/rand"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// A second Render with nothing changed must reuse the last frame: no
// allocations at all.
func TestRenderReusesUnchangedFrame(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(5)), 120, 30, 20, false)
	first := g.Render()
	if a := testing.AllocsPerRun(20, func() { _ = g.Render() }); a != 0 {
		t.Fatalf("unchanged grid: %v allocs per Render, want 0 (frame not reused)", a)
	}
	if got := g.Render(); got != first {
		t.Fatal("reused frame differs from the first one")
	}
	// Events that draw nothing keep the frame too.
	g.Apply([][]any{{"flush"}, {"mode_change", []any{"insert", 1}}})
	if a := testing.AllocsPerRun(20, func() { _ = g.Render() }); a != 0 {
		t.Fatalf("after flush: %v allocs per Render, want 0", a)
	}
}

// Every change that can show on screen must give a fresh frame, equal to
// what the old renderer draws.
func TestRenderFrameCacheInvalidates(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(9)), 40, 6, 5, false)
	g.CursorRow, g.CursorCol = 0, 0
	steps := []struct {
		name string
		do   func()
	}{
		{"grid_line", func() {
			g.Apply([][]any{{"grid_line", []any{1, 2, 3, []any{[]any{"Z", 1, 4}}}}})
		}},
		{"grid_scroll", func() { g.Apply([][]any{{"grid_scroll", []any{1, 0, 6, 0, 40, 1, 0}}}) }},
		{"grid_clear", func() { g.Apply([][]any{{"grid_clear", []any{1}}}) }},
		{"grid_line again", func() {
			g.Apply([][]any{{"grid_line", []any{1, 0, 0, []any{[]any{"hello", 2}, []any{" ", 3, 5}}}}})
		}},
		{"cursor move", func() { g.Apply([][]any{{"grid_cursor_goto", []any{1, 0, 1}}}) }},
		{"cursor field", func() { g.CursorRow, g.CursorCol = 0, 3 }},
		{"hl_attr_define", func() {
			g.Apply([][]any{{"hl_attr_define", []any{2, map[string]any{"foreground": 0x00ff00, "bold": true}, map[string]any{}, []any{}}}})
		}},
		{"default_colors_set", func() {
			g.Apply([][]any{{"default_colors_set", []any{0x000000, 0xffffff, -1, 0, 0}}})
		}},
		{"term palette", func() {
			g.HlAttrs[3] = Attrs{Fg: 0x123456, Bg: -1, Sp: -1, TermFg: 2}
			g.Invalidate()
			g.SetTermPalette([16]int{1: 0xabcdef})
		}},
		{"palette again", func() { g.SetTermPalette([16]int{1: 0x111111}) }},
		{"profile", func() { lipgloss.SetColorProfile(termenv.ANSI) }},
		{"resize", func() { g.Apply([][]any{{"grid_resize", []any{1, 30, 4}}}) }},
		{"invalidate after direct write", func() {
			g.Cells[0][0] = Cell{Rune: 'Q', HlID: 2}
			g.Invalidate()
		}},
	}
	prev := g.Render()
	for _, s := range steps {
		s.do()
		want := legacyRender(g)
		got := g.Render()
		if got != want {
			t.Fatalf("after %s: stale frame\n got %q\nwant %q", s.name, got, want)
		}
		if got == prev {
			t.Fatalf("after %s: frame did not change (bad test step)", s.name)
		}
		prev = got
	}
}

// BenchmarkGridRenderUnchanged renders a busy 200x50 screen again with
// nothing changed: a hover or a toast tick.
func BenchmarkGridRenderUnchanged(b *testing.B) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	g := randomGrid(rand.New(rand.NewSource(1)), 200, 50, 40, false)
	_ = g.Render()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Render()
	}
}

// BenchmarkGridRenderOneLine changes one line between frames (typing).
func BenchmarkGridRenderOneLine(b *testing.B) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	g := randomGrid(rand.New(rand.NewSource(1)), 200, 50, 40, false)
	ev := [][]any{{"grid_line", []any{1, 10, 4, []any{[]any{"x", 3}}}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Apply(ev)
		_ = g.Render()
	}
}
