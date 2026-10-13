package grid

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// legacyRender is the renderer as it was before the style cache: a fresh
// lipgloss style per run, every frame. Kept only as the reference the
// cached renderer must match byte for byte.
func legacyRender(g *Grid) string {
	if g.Width == 0 || g.Height == 0 {
		return ""
	}
	var sb strings.Builder
	for r := 0; r < g.Height; r++ {
		c := 0
		for c < g.Width {
			cur := r == g.CursorRow && c == g.CursorCol
			if cur {
				sb.WriteString(legacyRenderCell(g, r, c, true))
				c++
				continue
			}
			j := c + 1
			for j < g.Width &&
				g.Cells[r][j].HlID == g.Cells[r][c].HlID &&
				!(r == g.CursorRow && j == g.CursorCol) {
				j++
			}
			var text strings.Builder
			for k := c; k < j; k++ {
				if g.Cells[r][k].Cont {
					continue
				}
				rn := g.Cells[r][k].Rune
				if rn == 0 {
					rn = ' '
				}
				text.WriteRune(rn)
			}
			style := legacyStyleFor(g, g.Cells[r][c].HlID, false)
			sb.WriteString(style.Render(text.String()))
			c = j
		}
		if r < g.Height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func legacyRenderCell(g *Grid, r, c int, cursor bool) string {
	cell := g.Cells[r][c]
	if cell.Cont {
		return ""
	}
	rn := cell.Rune
	if rn == 0 {
		rn = ' '
	}
	return legacyStyleFor(g, cell.HlID, cursor).Render(string(rn))
}

func legacyStyleFor(g *Grid, hlID int, cursor bool) lipgloss.Style {
	a, ok := g.HlAttrs[hlID]
	if !ok {
		a = g.HlAttrs[0]
	}
	fg := a.Fg
	if a.TermFg > 0 && g.TermPaletteSet {
		fg = g.TermPalette[a.TermFg-1]
	}
	if fg < 0 {
		fg = g.DefaultFg
	}
	bg := a.Bg
	if a.TermBg > 0 && g.TermPaletteSet {
		bg = g.TermPalette[a.TermBg-1]
	}
	if bg < 0 {
		bg = g.DefaultBg
	}
	if a.Reverse {
		fg, bg = bg, fg
	}
	if cursor {
		fg, bg = bg, fg
	}
	s := lipgloss.NewStyle()
	if fg >= 0 {
		s = s.Foreground(lipgloss.Color(fmt.Sprintf("#%06x", fg&0xffffff)))
	}
	if bg >= 0 {
		s = s.Background(lipgloss.Color(fmt.Sprintf("#%06x", bg&0xffffff)))
	}
	if a.Bold {
		s = s.Bold(true)
	}
	if a.Italic {
		s = s.Italic(true)
	}
	if a.Underline {
		s = s.Underline(true)
	}
	if a.Strikethrough {
		s = s.Strikethrough(true)
	}
	return s
}
