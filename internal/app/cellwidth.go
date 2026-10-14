package app

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

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
	// One exact-size buffer: the frame is large and built every draw.
	size := h
	for i, c := range cols {
		size += len(c) - (len(lines[i]) - 1) + maxW[i]*h
		for _, w := range widths[i] {
			size -= w
		}
	}
	var b strings.Builder
	b.Grow(size)
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
			writeSpaces(&b, maxW[i]-w)
		}
	}
	return b.String()
}

// spaces backs writeSpaces (padding without a strings.Repeat per row).
const spaces = "                                                                                                                                "

// writeSpaces writes n spaces to b (nothing when n <= 0).
func writeSpaces(b *strings.Builder, n int) {
	for n > 0 {
		k := min(n, len(spaces))
		b.WriteString(spaces[:k])
		n -= k
	}
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
	size := len(lines)
	for i, l := range lines {
		size += len(l) + maxW - widths[i]
	}
	var b strings.Builder
	b.Grow(size)
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
		writeSpaces(&b, maxW-widths[i])
	}
	return b.String()
}

// rowWidths remembers the cell width of frame rows for the frame
// pipeline (renderBaseLines): a row is measured by several steps (clip,
// stack join, column join, normalize), and rows from cached panels are
// the same strings frame after frame. Entries are keyed by the string's
// data pointer and hold the string, so the pointer cannot be reused while
// the entry exists. Two generations: nextFrameWidths drops entries not
// used in the last two frames, so the memo stays about a frame in size.
var rowWidths struct {
	sync.Mutex
	cur, prev map[*byte]rowWidth
}

type rowWidth struct {
	s string
	w int
}

// minMemoRow: shorter rows are measured directly (a map lookup costs
// about as much).
const minMemoRow = 48

// frameWidth is cellWidth(s) through rowWidths.
func frameWidth(s string) int {
	if strings.IndexByte(s, '\n') >= 0 {
		return textWidth(s)
	}
	if len(s) < minMemoRow {
		return cellWidth(s)
	}
	k := unsafe.StringData(s)
	rowWidths.Lock()
	if e, ok := rowWidths.cur[k]; ok && len(e.s) == len(s) {
		rowWidths.Unlock()
		return e.w
	}
	if e, ok := rowWidths.prev[k]; ok && len(e.s) == len(s) {
		rowWidths.cur[k] = e
		rowWidths.Unlock()
		return e.w
	}
	rowWidths.Unlock()
	w := cellWidth(s)
	setFrameWidth(s, w)
	return w
}

// setFrameWidth records that row s is w cells wide (a row the pipeline
// just built, so its width is known without measuring).
func setFrameWidth(s string, w int) {
	if len(s) < minMemoRow {
		return
	}
	rowWidths.Lock()
	if rowWidths.cur == nil {
		rowWidths.cur = make(map[*byte]rowWidth)
	}
	rowWidths.cur[unsafe.StringData(s)] = rowWidth{s, w}
	rowWidths.Unlock()
}

// nextFrameWidths starts a new generation of rowWidths.
func nextFrameWidths() {
	rowWidths.Lock()
	rowWidths.prev, rowWidths.cur = rowWidths.cur, make(map[*byte]rowWidth, len(rowWidths.cur))
	rowWidths.Unlock()
}

// joinColumnsLines is joinColumns on columns given as rows: the result
// is joinColumns(cols...) split into rows. All rows share one buffer.
func joinColumnsLines(cols ...[]string) []string {
	if len(cols) == 0 {
		return []string{""}
	}
	if len(cols) == 1 {
		return cols[0]
	}
	h := 0
	maxW := make([]int, len(cols))
	widths := make([][]int, len(cols))
	size := 0
	for i, c := range cols {
		widths[i] = make([]int, len(c))
		for j, l := range c {
			w := frameWidth(l)
			widths[i][j] = w
			maxW[i] = max(maxW[i], w)
			size += len(l) - w
		}
		h = max(h, len(c))
	}
	for i := range cols {
		size += maxW[i] * h
	}
	var b strings.Builder
	b.Grow(size)
	ends := make([]int, h)
	for y := 0; y < h; y++ {
		for i, c := range cols {
			w := 0
			if y < len(c) {
				b.WriteString(c[y])
				w = widths[i][y]
			}
			writeSpaces(&b, maxW[i]-w)
		}
		ends[y] = b.Len()
	}
	all := b.String()
	out := make([]string, h)
	start, total := 0, 0
	for _, w := range maxW {
		total += w
	}
	for y, e := range ends {
		out[y] = all[start:e]
		start = e
		setFrameWidth(out[y], total)
	}
	return out
}

// joinRowsLines is joinRows on blocks given as rows: the result is
// joinRows(blocks...) split into rows.
func joinRowsLines(blocks ...[]string) []string {
	if len(blocks) == 0 {
		return []string{""}
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	n := 0
	for _, bl := range blocks {
		n += len(bl)
	}
	lines := make([]string, 0, n)
	for _, bl := range blocks {
		lines = append(lines, bl...)
	}
	widths := make([]int, len(lines))
	maxW := 0
	for i, l := range lines {
		widths[i] = frameWidth(l)
		maxW = max(maxW, widths[i])
	}
	size := 0
	for i := range lines {
		if widths[i] < maxW {
			size += maxW - widths[i]
		}
	}
	if size == 0 {
		return lines
	}
	// Pad the short rows; one buffer holds all the padded rows.
	for i, l := range lines {
		if widths[i] < maxW {
			size += len(l)
		}
	}
	var b strings.Builder
	b.Grow(size)
	type span struct{ i, start, end int }
	spans := make([]span, 0, len(lines))
	for i, l := range lines {
		if pad := maxW - widths[i]; pad > 0 {
			st := b.Len()
			b.WriteString(l)
			writeSpaces(&b, pad)
			spans = append(spans, span{i, st, b.Len()})
		}
	}
	all := b.String()
	for _, sp := range spans {
		lines[sp.i] = all[sp.start:sp.end]
		setFrameWidth(lines[sp.i], maxW)
	}
	return lines
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
