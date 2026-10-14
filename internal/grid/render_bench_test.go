package grid

import (
	"math/rand"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// randomGrid builds a w×h grid with nhl highlight groups covering the
// attribute space (colours, defaults, bold/italic/underline/undercurl/
// strikethrough/reverse, terminal palette colours), runs of varying length,
// wide glyphs, odd whitespace and a cursor.
func randomGrid(rng *rand.Rand, w, h, nhl int, tabs bool) *Grid {
	g := New()
	g.Apply([][]any{{"grid_resize", []any{1, w, h}}})
	switch rng.Intn(3) {
	case 0:
		g.Apply([][]any{{"default_colors_set", []any{0xd4d4d4, 0x1e1e1e, 0xff0000, 0, 0}}})
	case 1:
		g.Apply([][]any{{"default_colors_set", []any{-1, 0x101010, -1, 0, 0}}})
	}
	if rng.Intn(2) == 0 {
		for i := range g.TermPalette {
			g.TermPalette[i] = rng.Intn(0x1000000)
		}
		g.TermPaletteSet = true
	}
	color := func() int {
		if rng.Intn(4) == 0 {
			return -1
		}
		return rng.Intn(0x1000000)
	}
	for id := 1; id <= nhl; id++ {
		a := Attrs{
			Fg: color(), Bg: color(), Sp: color(),
			Bold:          rng.Intn(4) == 0,
			Italic:        rng.Intn(4) == 0,
			Underline:     rng.Intn(5) == 0,
			Undercurl:     rng.Intn(5) == 0,
			Strikethrough: rng.Intn(8) == 0,
			Reverse:       rng.Intn(6) == 0,
		}
		if rng.Intn(6) == 0 {
			a.TermFg = 1 + rng.Intn(16)
		}
		if rng.Intn(6) == 0 {
			a.TermBg = 1 + rng.Intn(16)
		}
		g.HlAttrs[id] = a
	}
	glyphs := []rune("abcdefghijklmnopqrstuvwxyz0123456789 ._-(){}[]=+*/   　ñé")
	for r := 0; r < h; r++ {
		c := 0
		for c < w {
			hl := rng.Intn(nhl + 2) // nhl+1 is undefined: falls back to hl 0
			n := 1 + rng.Intn(12)
			for k := 0; k < n && c < w; k++ {
				if rng.Intn(25) == 0 && c+1 < w {
					g.Cells[r][c] = Cell{Rune: '界', HlID: hl}
					g.Cells[r][c+1] = Cell{Rune: ' ', HlID: hl, Cont: true}
					c += 2
					continue
				}
				rn := glyphs[rng.Intn(len(glyphs))]
				switch rng.Intn(60) {
				case 0:
					rn = 0
				case 1:
					if tabs {
						rn = '\t' // nvim expands tabs; only odd input has them
					}
				}
				g.Cells[r][c] = Cell{Rune: rn, HlID: hl}
				c++
			}
		}
	}
	g.CursorRow, g.CursorCol = rng.Intn(h), rng.Intn(w)
	return g
}

// BenchmarkGridRender renders a busy 200x50 nvim screen.
func BenchmarkGridRender(b *testing.B) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	g := randomGrid(rand.New(rand.NewSource(1)), 200, 50, 40, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Invalidate() // a full draw every time, not the reused frame
		_ = g.Render()
	}
}

// BenchmarkGridRenderLegacy is the same screen through the old renderer,
// for comparison.
func BenchmarkGridRenderLegacy(b *testing.B) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	g := randomGrid(rand.New(rand.NewSource(1)), 200, 50, 40, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = legacyRender(g)
	}
}
