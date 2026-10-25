package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// A small cell canvas for the Itermoc detail pages: text, boxes, arrows and
// pills are put at cell positions, then every row is turned into one styled
// string. Colours are theme tokens, so a page follows the theme.

// imSt is the style of a run of cells. bg "" = the page background.
type imSt struct {
	fg, bg lipgloss.Color
	bold   bool
	it     bool
}

func imFg(tok theme.Color256) imSt { return imSt{fg: theme.LG(tok)} }

func (s imSt) B() imSt { s.bold = true; return s }
func (s imSt) I() imSt { s.it = true; return s }
func (s imSt) On(tok theme.Color256) imSt {
	s.bg = theme.LG(tok)
	return s
}

type imCell struct {
	r    rune
	st   imSt
	cont bool // the second half of a wide rune
}

// imCanvas is a w×h grid of cells.
type imCanvas struct {
	w, h  int
	bg    lipgloss.Color
	cells [][]imCell
}

func newImCanvas(w, h int, bg theme.Color256) *imCanvas {
	c := &imCanvas{w: w, h: h, bg: theme.LG(bg)}
	c.cells = make([][]imCell, h)
	for y := range c.cells {
		c.cells[y] = make([]imCell, w)
		for x := range c.cells[y] {
			c.cells[y][x] = imCell{r: ' ', st: imSt{bg: c.bg}}
		}
	}
	return c
}

func (c *imCanvas) in(x, y int) bool { return x >= 0 && y >= 0 && x < c.w && y < c.h }

// put sets one rune; a style without a background keeps the page's.
func (c *imCanvas) put(x, y int, r rune, st imSt) {
	if !c.in(x, y) {
		return
	}
	if st.bg == "" {
		st.bg = c.bg
	}
	c.cells[y][x] = imCell{r: r, st: st}
	if runewidth.RuneWidth(r) == 2 && c.in(x+1, y) {
		c.cells[y][x+1] = imCell{r: ' ', st: st, cont: true}
	}
}

// text writes s from (x, y) and returns the cell after it.
func (c *imCanvas) text(x, y int, s string, st imSt) int {
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		c.put(x, y, r, st)
		x += w
	}
	return x
}

// textR writes s so that it ends at xEnd (exclusive).
func (c *imCanvas) textR(xEnd, y int, s string, st imSt) int {
	return c.text(xEnd-runewidth.StringWidth(s), y, s, st)
}

// textC centres s in [x, x+w).
func (c *imCanvas) textC(x, y, w int, s string, st imSt) {
	sw := runewidth.StringWidth(s)
	c.text(x+max((w-sw)/2, 0), y, s, st)
}

func (c *imCanvas) hline(x, y, w int, r rune, st imSt) {
	for i := 0; i < w; i++ {
		c.put(x+i, y, r, st)
	}
}

// fill gives the rectangle a background (text stays).
func (c *imCanvas) fill(x, y, w, h int, bg lipgloss.Color) {
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			if c.in(x+i, y+j) {
				c.cells[y+j][x+i].st.bg = bg
			}
		}
	}
}

// box draws a rounded frame (square in ASCII icon mode).
func (c *imCanvas) box(x, y, w, h int, st imSt) {
	tl, tr, bl, br, hz, vt := '╭', '╮', '╰', '╯', '─', '│'
	if theme.CurrentIconMode == theme.IconModeASCII {
		tl, tr, bl, br, hz, vt = '+', '+', '+', '+', '-', '|'
	}
	c.hline(x+1, y, w-2, hz, st)
	c.hline(x+1, y+h-1, w-2, hz, st)
	for j := 1; j < h-1; j++ {
		c.put(x, y+j, vt, st)
		c.put(x+w-1, y+j, vt, st)
	}
	c.put(x, y, tl, st)
	c.put(x+w-1, y, tr, st)
	c.put(x, y+h-1, bl, st)
	c.put(x+w-1, y+h-1, br, st)
}

// pill draws a chip: half blocks at both ends, the text on the chip colour.
// Returns the cell after it. In ASCII mode the ends are [ and ].
func (c *imCanvas) pill(x, y int, label string, chip theme.Color256, fg theme.Color256, bold bool) int {
	l, r := '▐', '▌'
	if theme.CurrentIconMode == theme.IconModeASCII {
		l, r = '[', ']'
	}
	end := imSt{fg: theme.LG(chip)}
	if theme.CurrentIconMode == theme.IconModeASCII {
		end = imSt{fg: theme.LG(fg), bg: theme.LG(chip)}
	}
	c.put(x, y, l, end)
	x++
	x = c.text(x, y, label, imSt{fg: theme.LG(fg), bg: theme.LG(chip), bold: bold})
	c.put(x, y, r, end)
	return x + 1
}

// pillW is the width of a pill with this label.
func imPillW(label string) int { return runewidth.StringWidth(label) + 2 }

// row renders row y as one styled string.
func (c *imCanvas) row(y int) string {
	var sb strings.Builder
	line := c.cells[y]
	for x := 0; x < len(line); {
		st := line[x].st
		var run strings.Builder
		for x < len(line) && line[x].st == st {
			if !line[x].cont {
				run.WriteRune(line[x].r)
			}
			x++
		}
		s := lipgloss.NewStyle().Foreground(st.fg).Background(st.bg)
		if st.fg == "" {
			s = lipgloss.NewStyle().Background(st.bg)
		}
		if st.bold {
			s = s.Bold(true)
		}
		if st.it {
			s = s.Italic(true)
		}
		sb.WriteString(s.Render(run.String()))
	}
	return sb.String()
}

// rows renders rows [from, from+n).
func (c *imCanvas) rows(from, n int) []string {
	out := make([]string, 0, n)
	for y := from; y < from+n; y++ {
		if y >= 0 && y < c.h {
			out = append(out, c.row(y))
		} else {
			out = append(out, lipgloss.NewStyle().Background(c.bg).Render(strings.Repeat(" ", c.w)))
		}
	}
	return out
}
