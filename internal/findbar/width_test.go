package findbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every row of the panel is exactly PanelWidth cells, the same width the
// host places it with (Bounds). The menu glyph was "☰", which width
// tables disagree on (1 or 2 cells): the panel came out one cell wider
// than PanelWidth and its right border did not line up.
func TestPanelIsExactlyPanelWidth(t *testing.T) {
	for _, w := range []int{50, 66, 100} {
		for _, replace := range []bool{false, true} {
			m := New()
			m.SetWidth(w)
			m.SetQuery("needle")
			m.EnableReplace(replace)
			for i, l := range strings.Split(m.View(), "\n") {
				if got := ansi.StringWidth(l); got != m.PanelWidth() {
					t.Errorf("width %d replace %v: row %d is %d cells, PanelWidth %d: %q", w, replace, i, got, m.PanelWidth(), ansi.Strip(l))
				}
			}
		}
	}
}
