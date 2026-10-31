package prompt

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSmallScreenWidth(t *testing.T) {
	for w := 12; w <= 40; w++ {
		m := New(strings.Repeat("Title ", 20), "Label", strings.Repeat("value", 20))
		m.SetSize(w, 30)
		if w%2 == 0 {
			m.SetMultiline()
		}
		view := m.Box()
		for _, line := range strings.Split(view, "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Fatalf("screen %d: row width %d: %q", w, got, ansi.Strip(line))
			}
		}
	}
}
