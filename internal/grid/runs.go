package grid

import "unicode/utf16"

// Run is a styled run of one grid row for a viewer that draws the text
// itself (the web terminal view, WEB-40): From and To are UTF-16 columns
// of the row's text; Fg and Bg are 24-bit RGB or -1 (the viewer's own
// colour). Only runs drawn with something other than the default style
// are listed.
type Run struct {
	From, To  int
	Fg, Bg    int
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
}

// RowRuns returns the text of cells [c0, c1) of row r (the second cell
// of a wide glyph adds nothing) and its styled runs. A row outside the
// grid is "" and no runs.
func (g *Grid) RowRuns(r, c0, c1 int) (string, []Run) {
	if r < 0 || r >= len(g.Cells) {
		return "", nil
	}
	row := g.Cells[r]
	c0, c1 = max(c0, 0), min(c1, len(row))
	var text []rune
	var runs []Run
	at := 0
	for c := c0; c < c1; c++ {
		cell := row[c]
		if cell.Cont {
			continue
		}
		glyph := append([]rune{cellRune(cell)}, []rune(cell.Tail)...)
		n := len(utf16.Encode(glyph))
		text = append(text, glyph...)
		if st, ok := g.webStyle(cell.HlID); ok {
			if k := len(runs) - 1; k >= 0 && runs[k].To == at && sameStyle(runs[k], st) {
				runs[k].To += n
			} else {
				st.From, st.To = at, at+n
				runs = append(runs, st)
			}
		}
		at += n
	}
	return string(text), runs
}

func sameStyle(a, b Run) bool {
	a.From, a.To, b.From, b.To = 0, 0, 0, 0
	return a == b
}

// webStyle resolves hl id to a run's style; ok is false for the default
// style (nothing to draw).
func (g *Grid) webStyle(id int) (Run, bool) {
	a, ok := g.HlAttrs[id]
	if !ok {
		return Run{}, false
	}
	fg, bg := a.Fg, a.Bg
	if a.TermFg > 0 && g.TermPaletteSet {
		fg = g.TermPalette[a.TermFg-1]
	}
	if a.TermBg > 0 && g.TermPaletteSet {
		bg = g.TermPalette[a.TermBg-1]
	}
	if a.Reverse {
		if fg < 0 {
			fg = g.DefaultFg
		}
		if bg < 0 {
			bg = g.DefaultBg
		}
		fg, bg = bg, fg
	}
	norm := func(c int) int {
		if c < 0 {
			return -1
		}
		return c & 0xffffff
	}
	st := Run{Fg: norm(fg), Bg: norm(bg), Bold: a.Bold, Italic: a.Italic, Underline: a.Underline || a.Undercurl, Strike: a.Strikethrough}
	return st, st != (Run{Fg: -1, Bg: -1})
}
