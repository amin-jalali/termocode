package tea

// termocode patch (perf): a changed line is drawn again only from its
// first changed cell to its last one, not as the whole line. Typing a
// letter changed two lines of ~1 KB each (the edited row and the status
// bar), and every one of their bytes crossed the link; now only the few
// changed cells (with their colours) do.

import (
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// lineCell is one glyph of a line.
type lineCell struct {
	col, w     int    // first column and width in cells
	start, end int    // the glyph's bytes in the line
	state      string // the SGR sequences in effect (since the last reset)
}

func (a lineCell) same(b lineCell, as, bs string) bool {
	return a.col == b.col && a.w == b.w && a.state == b.state && as[a.start:a.end] == bs[b.start:b.end]
}

// parseLine splits a line into glyphs. ok is false for anything but SGR
// sequences and plain text whose width is sure (no control bytes, no
// other escape sequences, no runes that join with their neighbours).
func parseLine(s string) (cells []lineCell, width int, ok bool) {
	cells = make([]lineCell, 0, len(s)/8+8)
	state := ""    // the sequences in effect
	stateEnd := -1 // where state ends in s when it is one piece of s
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			if i+1 >= len(s) || s[i+1] != '[' {
				return nil, 0, false
			}
			j := i + 2
			for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
				j++
			}
			if j >= len(s) || s[j] != 'm' {
				return nil, 0, false
			}
			seq := s[i : j+1]
			switch {
			case seq == "\x1b[0m" || seq == "\x1b[m":
				state, stateEnd = "", -1
			case state == "":
				state, stateEnd = seq, j+1
			case stateEnd == i: // right after the last one: still one piece
				state, stateEnd = s[stateEnd-len(state):j+1], j+1
			default:
				state, stateEnd = state+seq, -1
			}
			i = j + 1
			continue
		}
		if c < 0x80 {
			if c < 0x20 || c == 0x7f {
				return nil, 0, false
			}
			cells = append(cells, lineCell{col: width, w: 1, start: i, end: i + 1, state: state})
			width++
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if r < 0xa0 || r == utf8.RuneError {
			return nil, 0, false
		}
		w := runeWidth(r, s[i:i+n]) // 0 for runes that may join others
		if w < 1 {
			return nil, 0, false
		}
		cells = append(cells, lineCell{col: width, w: w, start: i, end: i + n, state: state})
		width += w
		i += n
	}
	return cells, width, true
}

// runeWidths remembers runeWidth.
var runeWidths sync.Map // rune → int

// runeWidth is ansi.StringWidth of the one rune r (its bytes g), or 0
// when r may join its neighbours into one cell.
func runeWidth(r rune, g string) int {
	if v, ok := runeWidths.Load(r); ok {
		return v.(int)
	}
	w := 0
	if !joins(r) {
		w = ansi.StringWidth(g)
	}
	runeWidths.Store(r, w)
	return w
}

// joins reports runes that may form one cell with their neighbours.
func joins(r rune) bool {
	switch {
	case r == 0x200d, r == 0x200c,
		r >= 0xfe00 && r <= 0xfe0f,
		r >= 0xe0100 && r <= 0xe01ef,
		r >= 0x1f3fb && r <= 0x1f3ff,
		r >= 0x1f1e6 && r <= 0x1f1ff,
		r >= 0xe0020 && r <= 0xe007f,
		r >= 0x1100 && r <= 0x11ff,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xd7b0 && r <= 0xd7ff:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc)
}

// lineUpdate returns what turns the screen line old (as last drawn, the
// cursor at its first column) into new, when both are exactly width
// cells: a move to the first changed cell, its style, and the line's own
// bytes up to the last changed cell. ok is false when the line must be
// drawn whole (other escape sequences, another width, or no saving).
func lineUpdate(old, new string, width int) (string, bool) {
	oc, ow, ok := parseLine(old)
	if !ok || ow != width {
		return "", false
	}
	nc, nw, ok := parseLine(new)
	if !ok || nw != width {
		return "", false
	}
	p := 0
	for p < len(nc) && p < len(oc) && nc[p].same(oc[p], new, old) {
		p++
	}
	if p == len(nc) && p == len(oc) {
		return "", true // the same cells: nothing to draw
	}
	i, j := len(nc)-1, len(oc)-1
	for i >= p && j >= p && nc[i].same(oc[j], new, old) {
		i--
		j--
	}
	if i < p {
		return "", false // only cells went away: draw it whole
	}
	var b strings.Builder
	if col := nc[p].col; col > 0 {
		b.WriteString("\x1b[")
		b.WriteString(strconv.Itoa(col))
		b.WriteByte('C')
	}
	b.WriteString("\x1b[0m")
	b.WriteString(nc[p].state)
	b.WriteString(new[nc[p].start:nc[i].end])
	b.WriteString("\x1b[0m")
	if b.Len() >= len(new) {
		return "", false
	}
	return b.String(), true
}
