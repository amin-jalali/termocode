package grid

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Render returns the grid as a styled string suitable for use in a
// Bubble Tea View. The cursor cell (CursorRow, CursorCol) is drawn in
// reverse video.
func (g *Grid) Render() string {
	if g.Width == 0 || g.Height == 0 {
		return ""
	}

	var sb strings.Builder
	for r := 0; r < g.Height; r++ {
		c := 0
		for c < g.Width {
			cur := r == g.CursorRow && c == g.CursorCol
			if cur {
				sb.WriteString(g.renderCell(r, c, true))
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
				rn := g.Cells[r][k].Rune
				if rn == 0 {
					rn = ' '
				}
				text.WriteRune(rn)
			}
			style := g.styleFor(g.Cells[r][c].HlID, false)
			sb.WriteString(style.Render(text.String()))
			c = j
		}
		if r < g.Height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (g *Grid) renderCell(r, c int, cursor bool) string {
	cell := g.Cells[r][c]
	rn := cell.Rune
	if rn == 0 {
		rn = ' '
	}
	style := g.styleFor(cell.HlID, cursor)
	return style.Render(string(rn))
}

func (g *Grid) styleFor(hlID int, cursor bool) lipgloss.Style {
	a, ok := g.HlAttrs[hlID]
	if !ok {
		a = g.HlAttrs[0]
	}
	fg := a.Fg
	if fg < 0 {
		fg = g.DefaultFg
	}
	bg := a.Bg
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
		s = s.Foreground(rgbColor(fg))
	}
	if bg >= 0 {
		s = s.Background(rgbColor(bg))
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

func rgbColor(rgb int) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%06x", rgb&0xffffff))
}
