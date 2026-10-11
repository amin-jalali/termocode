package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// A splice must never cut a multi-byte glyph in half: broken UTF-8 makes the
// terminal draw extra cells, the line wraps and the whole screen scrolls.
func TestSpliceAtKeepsUTF8AndWidth(t *testing.T) {
	base := "\x1b[38;2;1;2;3m▌ commit ─ subject ▕\x1b[0m" + strings.Repeat("─", 30) + "│ 界界 end"
	w := ansi.StringWidth(base)
	for x := 0; x < w-6; x++ {
		got := spliceAt(base, "[card]", x)
		if !utf8.ValidString(got) {
			t.Fatalf("x=%d: invalid UTF-8: %q", x, got)
		}
		if gw := ansi.StringWidth(got); gw != w {
			t.Fatalf("x=%d: width %d, want %d: %q", x, gw, w, ansi.Strip(got))
		}
	}
}

// A toast sliding in from the right starts off screen: every frame of the
// slide must keep each row exactly screen-wide.
func TestModalOverlaySlideClipsAtRightEdge(t *testing.T) {
	const w, h = 80, 10
	row := strings.Repeat("·", w)
	base := strings.TrimSuffix(strings.Repeat(row+"\n", h), "\n")
	card := "╭────────╮\n│ toast 界│\n╰────────╯"
	for _, p := range []float64{0, 0.1, 0.3, 0.5, 0.8, 1} {
		o := ModalOpts{At: true, X: w - 12, Y: 3, NoDim: true, SlideX: 14, Animating: true, Progress: p}
		out := modalOverlay(base, card, w, h, &o)
		for i, r := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(r); got != w {
				t.Fatalf("progress %.1f: row %d is %d cells: %q", p, i, got, ansi.Strip(r))
			}
		}
	}
}
