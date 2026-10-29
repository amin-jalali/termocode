// Package cellwidth measures and cuts text in terminal cells.
//
// It has the API of github.com/mattn/go-runewidth (StringWidth,
// Truncate, TruncateLeft, FillRight, RuneWidth) but sizes each glyph
// (grapheme cluster) the way lipgloss and the frame checks do
// (charmbracelet/x/ansi): a flag (two regional indicators) or an emoji
// with a presentation selector is two cells, not one. go-runewidth takes
// the width of a cluster's first rune, so a row with a flag came out one
// cell too wide for each flag, and the whole frame shifted.
//
// A glyph is never narrower than go-runewidth says, so its CJK-locale
// setting (DefaultCondition.EastAsianWidth: ambiguous glyphs two cells)
// still works. Like go-runewidth, the functions see escape bytes as
// plain text; they are meant for text without styles.
package cellwidth

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// clusters calls f for each grapheme cluster of s with its byte offsets
// and width, until f returns false. Like ansi.StringWidth, an ASCII byte
// is a cell of its own (a control byte none).
func clusters(s string, f func(start, end, w int) bool) {
	for i := 0; i < len(s); {
		if b := s[i]; b < 0x80 {
			w := 0
			if b >= 0x20 && b < 0x7f {
				w = 1
			}
			if !f(i, i+1, w) {
				return
			}
			i++
			continue
		}
		c, w := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		// go-runewidth's width of the first rune that has one: it may be
		// wider, e.g. an ambiguous glyph in a CJK locale
		// (runewidth.DefaultCondition.EastAsianWidth). A cluster of
		// marks only (after an ASCII letter) stays zero-width.
		for _, r := range c {
			if w == 0 {
				break
			}
			if rw := runewidth.RuneWidth(r); rw > 0 {
				w = max(w, rw)
				break
			}
		}
		n := len(c)
		if n == 0 {
			n = 1
		}
		if !f(i, i+n, w) {
			return
		}
		i += n
	}
}

// StringWidth returns how many cells s takes.
func StringWidth(s string) (width int) {
	clusters(s, func(_, _, w int) bool { width += w; return true })
	return width
}

// RuneWidth returns how many cells r takes on its own.
func RuneWidth(r rune) int { return runewidth.RuneWidth(r) }

// Truncate cuts s to at most w cells, ending it with tail when it was
// cut. A wide glyph that does not fit is dropped whole.
func Truncate(s string, w int, tail string) string {
	if StringWidth(s) <= w {
		return s
	}
	w -= StringWidth(tail)
	width, pos := 0, len(s)
	clusters(s, func(start, _, cw int) bool {
		if width+cw > w {
			pos = start
			return false
		}
		width += cw
		return true
	})
	return s[:pos] + tail
}

// TruncateLeft cuts w cells from the start of s and puts prefix in
// front. When a wide glyph is cut in half, spaces stand in for its rest.
func TruncateLeft(s string, w int, prefix string) string {
	if StringWidth(s) <= w {
		return prefix
	}
	width, pos := 0, len(s)
	clusters(s, func(start, end, cw int) bool {
		if width+cw > w {
			if width < w {
				pos = end
				prefix += strings.Repeat(" ", width+cw-w)
			} else {
				pos = start
			}
			return false
		}
		width += cw
		return true
	})
	return prefix + s[pos:]
}

// FillRight pads s with spaces on the right to w cells.
func FillRight(s string, w int) string {
	if n := w - StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
