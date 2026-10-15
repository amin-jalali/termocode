package statusbar

import (
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// truncate used to cut by bytes, so a narrow window split "◆" (3 bytes) and
// wrote broken UTF-8 into the status bar (found by the e2e resize test at
// 40x12).
func TestTruncateKeepsGlyphsWhole(t *testing.T) {
	for _, s := range []string{"◆ 002", "⎇ ⌥ → — 界界界", "plain ascii text"} {
		for n := 1; n <= 12; n++ {
			got := truncate(s, n)
			if !utf8.ValidString(got) {
				t.Fatalf("truncate(%q, %d) = %q: invalid UTF-8", s, n, got)
			}
			if w := ansi.StringWidth(got); w > n {
				t.Fatalf("truncate(%q, %d) = %q: width %d", s, n, got, w)
			}
		}
	}
}

func TestNarrowStatusBarIsValidUTF8(t *testing.T) {
	m := New()
	s := State{Path: "/a/界.go", Project: "◆ 界界 proj", Lang: "Go", Line: 1, Col: 1, Branch: "feature/⎇-界"}
	for w := 4; w <= 60; w++ {
		m.SetWidth(w)
		out := m.View(s)
		if !utf8.ValidString(out) {
			t.Fatalf("width %d: invalid UTF-8 in %q", w, out)
		}
	}
}
