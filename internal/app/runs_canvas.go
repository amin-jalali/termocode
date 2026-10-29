package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// A small cell canvas for the Runs view and page: text, boxes, bars and
// pills are put at cell positions, then each row becomes one styled
// string. Colours are theme tokens (or blends of them), so the view
// follows the theme.

// rcSt is the style of a cell. bg "" keeps the cell's background.
type rcSt struct {
	fg, bg lipgloss.Color
	bold   bool
	it     bool
}

func rcFg(tok theme.Color256) rcSt { return rcSt{fg: theme.LG(tok)} }

func (s rcSt) B() rcSt                   { s.bold = true; return s }
func (s rcSt) I() rcSt                   { s.it = true; return s }
func (s rcSt) On(bg lipgloss.Color) rcSt { s.bg = bg; return s }

type rcCell struct {
	r    rune
	st   rcSt
	cont bool // the right half of a wide rune
}

// rcCanvas is a w×h grid of cells on one background.
type rcCanvas struct {
	w, h  int
	bg    lipgloss.Color
	cells [][]rcCell
}

func newRCCanvas(w, h int, bg lipgloss.Color) *rcCanvas {
	w, h = max(w, 0), max(h, 0)
	c := &rcCanvas{w: w, h: h, bg: bg, cells: make([][]rcCell, h)}
	for y := range c.cells {
		c.cells[y] = make([]rcCell, w)
		for x := range c.cells[y] {
			c.cells[y][x] = rcCell{r: ' ', st: rcSt{bg: bg}}
		}
	}
	return c
}

func (c *rcCanvas) in(x, y int) bool { return x >= 0 && y >= 0 && x < c.w && y < c.h }

func (c *rcCanvas) put(x, y int, r rune, st rcSt) {
	if !c.in(x, y) {
		return
	}
	if st.bg == "" {
		st.bg = c.cells[y][x].st.bg
	}
	if rw := runewidth.RuneWidth(r); rw == 2 {
		if !c.in(x+1, y) {
			return
		}
		c.cells[y][x+1] = rcCell{r: ' ', st: st, cont: true}
	}
	c.cells[y][x] = rcCell{r: r, st: st}
}

// text writes s from x and returns the cell after it.
func (c *rcCanvas) text(x, y int, s string, st rcSt) int {
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

// textMax writes s cut to n cells (with …).
func (c *rcCanvas) textMax(x, y int, s string, n int, st rcSt) int {
	if n <= 0 {
		return x
	}
	return c.text(x, y, rcCut(s, n), st)
}

// textR writes s so that it ends at xEnd (exclusive); returns its start.
func (c *rcCanvas) textR(xEnd, y int, s string, st rcSt) int {
	x := xEnd - runewidth.StringWidth(s)
	c.text(x, y, s, st)
	return x
}

// textC centres s in [x, x+w).
func (c *rcCanvas) textC(x, y, w int, s string, st rcSt) {
	c.text(x+max((w-runewidth.StringWidth(s))/2, 0), y, s, st)
}

func (c *rcCanvas) hline(x, y, w int, r rune, st rcSt) {
	for i := 0; i < w; i++ {
		c.put(x+i, y, r, st)
	}
}

// tint gives a rectangle a background (its text stays).
func (c *rcCanvas) tint(x, y, w, h int, bg lipgloss.Color) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			if c.in(i, j) {
				c.cells[j][i].st.bg = bg
			}
		}
	}
}

// box draws a rounded frame (square ASCII in ASCII icon mode).
func (c *rcCanvas) box(x, y, w, h int, st rcSt) {
	if w < 2 || h < 2 {
		return
	}
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

// pill draws a chip ▐ label ▌ ([label] in ASCII); returns the cell after it.
func (c *rcCanvas) pill(x, y int, label string, chip lipgloss.Color, fg lipgloss.Color, bold bool) int {
	l, r := '▐', '▌'
	end := rcSt{fg: chip}
	if theme.CurrentIconMode == theme.IconModeASCII {
		l, r = '[', ']'
		end = rcSt{fg: fg, bg: chip}
	}
	c.put(x, y, l, end)
	x = c.text(x+1, y, label, rcSt{fg: fg, bg: chip, bold: bold})
	c.put(x, y, r, end)
	return x + 1
}

// rcPillW is the width of a pill with this label.
func rcPillW(label string) int { return runewidth.StringWidth(label) + 2 }

// rcCut cuts s to n cells with … at the end.
func rcCut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= n {
		return s
	}
	return runewidth.Truncate(s, n, rcEllipsis())
}

func rcEllipsis() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "~"
	}
	return "…"
}

// row renders row y as one styled string.
func (c *rcCanvas) row(y int) string {
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
		s := lipgloss.NewStyle().Background(st.bg)
		if st.fg != "" {
			s = s.Foreground(st.fg)
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

// lines renders every row.
func (c *rcCanvas) lines() []string {
	out := make([]string, c.h)
	for y := range out {
		out[y] = c.row(y)
	}
	return out
}

// plain is row y's text without styles (tests and hit labels).
func (c *rcCanvas) plain(y int) string {
	if y < 0 || y >= c.h {
		return ""
	}
	var b strings.Builder
	for _, cl := range c.cells[y] {
		if !cl.cont {
			b.WriteRune(cl.r)
		}
	}
	return b.String()
}
