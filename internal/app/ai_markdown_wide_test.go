package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A long code line of CJK and flags wraps between glyphs: no row holds
// half a flag and no row is wider than the box.
func TestWrapStyledLineKeepsFlagsWhole(t *testing.T) {
	line := strings.Repeat("中文🇮🇷", 20)
	for _, w := range []int{9, 10, 11, 30} {
		for _, row := range wrapStyledLine([]mdSpan{{text: line}}, w) {
			var b strings.Builder
			for _, sp := range row {
				b.WriteString(sp.text)
			}
			s := b.String()
			if ansi.StringWidth(s) > w {
				t.Errorf("w=%d: row %q is %d cells", w, s, ansi.StringWidth(s))
			}
			if n := strings.Count(s, "🇮") - strings.Count(s, "🇮🇷"); n != 0 || strings.Count(s, "🇷") != strings.Count(s, "🇮🇷") {
				t.Errorf("w=%d: row %q cuts a flag", w, s)
			}
		}
	}
}
