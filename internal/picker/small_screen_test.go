package picker

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSmallScreenWidth(t *testing.T) {
	for w := 12; w <= 40; w++ {
		m := NewItems(strings.Repeat("Title ", 20), []Item{{ID: "a", Title: strings.Repeat("item ", 20)}})
		m.SetSize(w, 30)
		m.input = strings.Repeat("query", 20)
		view := m.Box()
		for _, line := range strings.Split(view, "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Fatalf("screen %d: row width %d: %q", w, got, ansi.Strip(line))
			}
		}
	}
}
