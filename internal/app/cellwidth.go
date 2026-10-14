package app

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// cellWidth is ansi.StringWidth, fast. Measuring every row of every panel
// on every frame was a fifth of the CPU while a key is held: the full
// parser walks a state table per byte. Rows are plain text, CSI colour
// codes and single runes, so those take a fast path (a rune's width is
// looked up once and remembered). Anything that can join runes into one
// cell (combining marks, ZWJ, variation selectors, skin tones, flags,
// Hangul jamo) or a non-CSI escape goes to the full parser, so the
// result always equals ansi.StringWidth.
func cellWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			j, ok := skipCSI(s, i)
			if !ok {
				return ansi.StringWidth(s)
			}
			i = j
		case c < 0x80:
			if c >= 0x20 && c != 0x7f {
				w++
			} else if c != '\n' && c != '\r' && c != '\t' {
				return ansi.StringWidth(s)
			}
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError || joinsRunes(r) {
				return ansi.StringWidth(s)
			}
			w += runeCells(r)
			i += n
		}
	}
	if strings.IndexByte(s, '\t') >= 0 || strings.IndexByte(s, '\n') >= 0 || strings.IndexByte(s, '\r') >= 0 {
		return ansi.StringWidth(s)
	}
	return w
}

// skipCSI returns the index after the CSI sequence at s[i] (ESC [ … final).
func skipCSI(s string, i int) (int, bool) {
	if i+1 >= len(s) || s[i+1] != '[' {
		return 0, false
	}
	for j := i + 2; j < len(s); j++ {
		c := s[j]
		switch {
		case c >= 0x40 && c <= 0x7e:
			return j + 1, true
		case c < 0x20 || c > 0x3f:
			return 0, false
		}
	}
	return 0, false
}

// joinsRunes reports runes that may merge with their neighbours into one
// grapheme (or are C1 controls); those strings take the full parser.
func joinsRunes(r rune) bool {
	switch {
	case r < 0xa0,
		r == 0x200d, r == 0x200c,
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

var runeCellCache sync.Map // rune → int

func runeCells(r rune) int {
	if v, ok := runeCellCache.Load(r); ok {
		return v.(int)
	}
	n := ansi.StringWidth(string(r))
	runeCellCache.Store(r, n)
	return n
}

// joinColumns is lipgloss.JoinHorizontal(lipgloss.Top, cols...) with
// cellWidth: each column's rows are padded with spaces to the column's
// widest row, and short columns get blank rows at the bottom.
func joinColumns(cols ...string) string {
	if len(cols) == 0 {
		return ""
	}
	if len(cols) == 1 {
		return cols[0]
	}
	lines := make([][]string, len(cols))
	widths := make([][]int, len(cols))
	maxW := make([]int, len(cols))
	h := 0
	for i, c := range cols {
		lines[i] = strings.Split(c, "\n")
		widths[i] = make([]int, len(lines[i]))
		for j, l := range lines[i] {
			w := cellWidth(l)
			widths[i][j] = w
			if w > maxW[i] {
				maxW[i] = w
			}
		}
		if len(lines[i]) > h {
			h = len(lines[i])
		}
	}
	var b strings.Builder
	b.Grow(len(cols[0]) * len(cols))
	for y := 0; y < h; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		for i := range cols {
			w := 0
			if y < len(lines[i]) {
				b.WriteString(lines[i][y])
				w = widths[i][y]
			}
			if pad := maxW[i] - w; pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	return b.String()
}

// joinRows is lipgloss.JoinVertical(lipgloss.Left, blocks...) with
// cellWidth: every row is padded with spaces to the widest row.
func joinRows(blocks ...string) string {
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	var lines []string
	for _, bl := range blocks {
		lines = append(lines, strings.Split(bl, "\n")...)
	}
	widths := make([]int, len(lines))
	maxW := 0
	for i, l := range lines {
		widths[i] = cellWidth(l)
		if widths[i] > maxW {
			maxW = widths[i]
		}
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
		if pad := maxW - widths[i]; pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return b.String()
}

// textWidth is lipgloss.Width (the widest line) with cellWidth.
func textWidth(s string) int {
	if strings.IndexByte(s, '\n') < 0 {
		return cellWidth(s)
	}
	w := 0
	for _, l := range strings.Split(s, "\n") {
		if lw := cellWidth(l); lw > w {
			w = lw
		}
	}
	return w
}
