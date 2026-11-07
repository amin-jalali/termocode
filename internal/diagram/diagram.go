package diagram

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// MaxSource is the biggest block drawn; bigger ones keep their source.
const MaxSource = 64 << 10

// maxRows caps the rows of one drawing.
const maxRows = 400

// Lang is the diagram kind of a fence info string ("mermaid", "csv",
// "tsv", "excalidraw"), or "" when the block is not a diagram.
func Lang(info string) string {
	f := strings.Fields(strings.ToLower(info))
	if len(f) == 0 {
		return ""
	}
	switch l := strings.Trim(f[0], "{}."); l {
	case "mermaid", "mmd":
		return "mermaid"
	case "csv", "tsv", "excalidraw":
		return l
	}
	return ""
}

// IsDiagram reports whether a fence info string names a diagram language.
func IsDiagram(info string) bool { return Lang(info) != "" }

// Render draws src as a diagram of the fence language lang, in rows no
// wider than width cells (right-trimmed). ok is false when lang is not a
// diagram language, src does not parse or the drawing does not fit.
func Render(lang, src string, width int) (rows []string, ok bool) {
	if width < 4 || len(src) > MaxSource || strings.TrimSpace(src) == "" {
		return nil, false
	}
	defer func() { // a drawing bug must never take the view down
		if recover() != nil {
			rows, ok = nil, false
		}
	}()
	switch Lang(lang) {
	case "mermaid":
		rows, ok = mermaid(src, width)
	case "csv":
		rows, ok = table(src, ',', width)
	case "tsv":
		rows, ok = table(src, '\t', width)
	case "excalidraw":
		rows, ok = excalidraw(src, width)
	}
	if !ok || len(rows) == 0 || len(rows) > maxRows {
		return nil, false
	}
	for _, r := range rows {
		if runewidth.StringWidth(r) > width {
			return nil, false
		}
	}
	return rows, true
}

// cleanText drops control characters (escapes, bidi overrides) from a
// label so a block can never style or move the terminal.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0,
			r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, s)
}

// cut shortens s to at most w cells with a "…".
func cut(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// Line bits of a canvas cell: which neighbours a line joins.
const (
	bUp uint8 = 1 << iota
	bDown
	bLeft
	bRight
)

// wideTail marks the second cell of a wide rune.
const wideTail rune = -1

// canvas is a grid of cells: a drawn rune wins over line bits.
type canvas struct {
	w, h int
	cell [][]rune
	bits [][]uint8
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cell: make([][]rune, h), bits: make([][]uint8, h)}
	for y := range c.cell {
		c.cell[y] = make([]rune, w)
		c.bits[y] = make([]uint8, w)
	}
	return c
}

func (c *canvas) in(x, y int) bool { return x >= 0 && y >= 0 && x < c.w && y < c.h }

// empty reports whether nothing is drawn at x, y.
func (c *canvas) empty(x, y int) bool {
	return c.in(x, y) && c.cell[y][x] == 0 && c.bits[y][x] == 0
}

func (c *canvas) set(x, y int, r rune) {
	if !c.in(x, y) {
		return
	}
	if old := c.cell[y][x]; old == wideTail && x > 0 {
		c.cell[y][x-1] = ' '
	}
	c.cell[y][x] = r
	if runewidth.RuneWidth(r) == 2 {
		if c.in(x+1, y) {
			c.cell[y][x+1] = wideTail
		} else {
			c.cell[y][x] = '…'
		}
	}
}

// text writes s from x, y; it stops at the canvas edge.
func (c *canvas) text(x, y int, s string) {
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			continue
		}
		c.set(x, y, r)
		x += rw
	}
}

// hline and vline add line bits from one cell to another (inclusive).
func (c *canvas) hline(y, x1, x2 int) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		if !c.in(x, y) {
			continue
		}
		if x > x1 {
			c.bits[y][x] |= bLeft
		}
		if x < x2 {
			c.bits[y][x] |= bRight
		}
		if x1 == x2 {
			c.bits[y][x] |= bLeft | bRight
		}
	}
}

func (c *canvas) vline(x, y1, y2 int) {
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		if !c.in(x, y) {
			continue
		}
		if y > y1 {
			c.bits[y][x] |= bUp
		}
		if y < y2 {
			c.bits[y][x] |= bDown
		}
		if y1 == y2 {
			c.bits[y][x] |= bUp | bDown
		}
	}
}

// line joins two cells with a straight line, or an elbow (across, then
// down) when they are on no common row or column.
func (c *canvas) line(x1, y1, x2, y2 int) {
	switch {
	case y1 == y2:
		c.hline(y1, x1, x2)
	case x1 == x2:
		c.vline(x1, y1, y2)
	default:
		c.hline(y1, x1, x2)
		c.vline(x2, y1, y2)
	}
}

func boxRune(b uint8) rune {
	switch b {
	case bUp | bDown, bUp, bDown:
		return '│'
	case bLeft | bRight, bLeft, bRight:
		return '─'
	case bDown | bRight:
		return '┌'
	case bDown | bLeft:
		return '┐'
	case bUp | bRight:
		return '└'
	case bUp | bLeft:
		return '┘'
	case bUp | bDown | bRight:
		return '├'
	case bUp | bDown | bLeft:
		return '┤'
	case bLeft | bRight | bDown:
		return '┬'
	case bLeft | bRight | bUp:
		return '┴'
	}
	return '┼'
}

// Box shapes.
const (
	shapeRect byte = iota
	shapeRound
	shapeDiamond
)

// box draws a w×h frame at x, y with lines centred inside it.
func (c *canvas) box(x, y, w, h int, shape byte, lines []string) {
	if w < 2 || h < 2 {
		return
	}
	tl, tr, bl, br := '┌', '┐', '└', '┘'
	switch shape {
	case shapeRound:
		tl, tr, bl, br = '╭', '╮', '╰', '╯'
	case shapeDiamond:
		tl, tr, bl, br = '╱', '╲', '╲', '╱'
	}
	for i := 1; i < w-1; i++ {
		c.set(x+i, y, '─')
		c.set(x+i, y+h-1, '─')
	}
	for j := 1; j < h-1; j++ {
		c.set(x, y+j, '│')
		c.set(x+w-1, y+j, '│')
		for i := 1; i < w-1; i++ {
			c.set(x+i, y+j, ' ')
		}
	}
	c.set(x, y, tl)
	c.set(x+w-1, y, tr)
	c.set(x, y+h-1, bl)
	c.set(x+w-1, y+h-1, br)
	inner := w - 2
	for j, l := range lines {
		if j >= h-2 {
			break
		}
		l = cut(l, inner)
		c.text(x+1+(inner-runewidth.StringWidth(l))/2, y+1+j, l)
	}
}

// rows returns the canvas as right-trimmed text rows.
func (c *canvas) rows() []string {
	out := make([]string, c.h)
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		b.Reset()
		for x := 0; x < c.w; x++ {
			switch r := c.cell[y][x]; {
			case r == wideTail:
				if x == 0 || runewidth.RuneWidth(c.cell[y][x-1]) != 2 {
					b.WriteByte(' ')
				}
			case r != 0:
				b.WriteRune(r)
			case c.bits[y][x] != 0:
				b.WriteRune(boxRune(c.bits[y][x]))
			default:
				b.WriteByte(' ')
			}
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}
