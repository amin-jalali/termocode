package findbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

// In a narrow editor (panel 36..45 cells) the input well shrinks, so the
// panel stays PanelWidth wide and the × button stays inside Bounds, where
// a click reaches it. Before, the well kept 8 cells: the panel grew past
// the pane and × fell outside Bounds (clicks on it were ignored).
func TestNarrowPanelKeepsItsWidthAndCloseButton(t *testing.T) {
	for _, w := range []int{38, 40, 44, 47} {
		for _, replace := range []bool{false, true} {
			m := New()
			m.SetWidth(w)
			m.SetQuery("needle")
			m.EnableReplace(replace)
			m.SetAnchor(3, 2)
			for i, l := range strings.Split(m.View(), "\n") {
				if got := ansi.StringWidth(l); got != m.PanelWidth() {
					t.Errorf("width %d replace %v: row %d is %d cells, PanelWidth %d", w, replace, i, got, m.PanelWidth())
				}
			}
			_, _, _, _, closeCol := m.buttonColumns()
			_, cmd := m.HandleMouse(3+closeCol, 3, tea.MouseActionPress, tea.MouseButtonLeft)
			if cmd == nil {
				t.Fatalf("width %d: click on × did nothing", w)
			}
			if _, ok := cmd().(CloseMsg); !ok {
				t.Errorf("width %d: click on × sent %T", w, cmd())
			}
		}
	}
}
