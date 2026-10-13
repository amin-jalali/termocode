package grid

import (
	"math/rand"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var allProfiles = []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii}

func withProfile(t testing.TB, p termenv.Profile) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func assertSameAsLegacy(t *testing.T, g *Grid, what string) {
	t.Helper()
	want := legacyRender(g)
	if got := g.Render(); got != want {
		t.Fatalf("%s: cached render differs from legacy\n got %q\nwant %q", what, got, want)
	}
}

// The cached renderer must write exactly what the old per-run lipgloss
// renderer wrote, over many random grids, in every colour profile.
func TestRenderMatchesLegacy(t *testing.T) {
	for _, p := range allProfiles {
		withProfile(t, p)
		rng := rand.New(rand.NewSource(int64(p) + 7))
		for i := 0; i < 60; i++ {
			g := randomGrid(rng, 1+rng.Intn(90), 1+rng.Intn(20), 1+rng.Intn(30), true)
			assertSameAsLegacy(t, g, p.Name())
			// Second frame comes from a warm cache.
			assertSameAsLegacy(t, g, p.Name()+" warm")
		}
	}
}

// Every attribute on its own, with and without the cursor on it.
func TestRenderMatchesLegacyEachAttr(t *testing.T) {
	attrs := []Attrs{
		{Fg: -1, Bg: -1, Sp: -1},
		{Fg: 0x112233, Bg: -1, Sp: -1},
		{Fg: -1, Bg: 0x445566, Sp: -1},
		{Fg: 0x1, Bg: 0xfffffe, Sp: -1, Bold: true},
		{Fg: 0xabcdef, Bg: 0x10, Sp: -1, Italic: true},
		{Fg: 0xabcdef, Bg: 0x10, Sp: 0xff0000, Underline: true},
		{Fg: 0xabcdef, Bg: 0x10, Sp: 0xff0000, Undercurl: true},
		{Fg: 0xabcdef, Bg: 0x10, Sp: -1, Strikethrough: true},
		{Fg: 0xabcdef, Bg: -1, Sp: -1, Reverse: true},
		{Fg: -1, Bg: -1, Sp: -1, Bold: true, Underline: true, Strikethrough: true, Reverse: true},
		{Fg: 0x2000000 | 0x123456, Bg: 0x654321, Sp: -1, TermFg: 3, TermBg: 5},
	}
	text := []any{
		[]any{"a", 1}, []any{" ", 1}, []any{"界", 1}, []any{"", 1},
		[]any{" ", 1}, []any{"b", 1}, []any{"\t", 1}, []any{"c", 1},
	}
	for _, p := range allProfiles {
		withProfile(t, p)
		for _, dfg := range []int{-1, 0xd4d4d4} {
			for i, a := range attrs {
				g := New()
				g.Apply([][]any{{"grid_resize", []any{1, 8, 1}}})
				g.Apply([][]any{{"default_colors_set", []any{dfg, 0x1e1e1e, -1, 0, 0}}})
				g.TermPalette[2], g.TermPalette[4] = 0x00ff00, 0x0000ff
				g.TermPaletteSet = i%2 == 0
				g.HlAttrs[1] = a
				g.Apply([][]any{{"grid_line", []any{1, 0, 0, text}}})
				for cur := -1; cur < 8; cur++ {
					g.CursorRow, g.CursorCol = 0, cur
					assertSameAsLegacy(t, g, p.Name())
				}
			}
		}
	}
}

// The cache must follow every input that changes colours between frames:
// hl_attr_define, default_colors_set, the terminal palette and the colour
// profile (which the terminal's reply to the caps query can change).
func TestRenderCacheFollowsChanges(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := randomGrid(rand.New(rand.NewSource(3)), 40, 6, 5, true)
	assertSameAsLegacy(t, g, "start")

	g.Apply([][]any{{"hl_attr_define", []any{1, map[string]any{"foreground": 0x00ff00, "bold": true}, map[string]any{}, []any{}}}})
	assertSameAsLegacy(t, g, "hl_attr_define")

	g.Apply([][]any{{"default_colors_set", []any{0x000000, 0xffffff, -1, 0, 0}}})
	assertSameAsLegacy(t, g, "default_colors_set")

	for i := range g.TermPalette {
		g.TermPalette[i] = 0x010101 * i
	}
	g.TermPaletteSet = true
	g.HlAttrs[2] = Attrs{Fg: 0x123456, Bg: -1, Sp: -1, TermFg: 9}
	assertSameAsLegacy(t, g, "term palette")

	for _, p := range []termenv.Profile{termenv.ANSI256, termenv.ANSI, termenv.Ascii, termenv.TrueColor} {
		lipgloss.SetColorProfile(p)
		assertSameAsLegacy(t, g, "profile "+p.Name())
	}
}

func TestRenderCacheIsBounded(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, 1, 1}}})
	g.CursorRow = -1
	for i := 0; i < maxStyles*2; i++ {
		g.HlAttrs[0] = Attrs{Fg: i, Bg: -1, Sp: -1}
		g.Render()
	}
	if n := len(g.styles); n > maxStyles+1 {
		t.Fatalf("style cache grew to %d entries, want <= %d", n, maxStyles+1)
	}
}
