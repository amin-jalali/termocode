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
