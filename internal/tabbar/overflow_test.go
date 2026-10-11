package tabbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// cutNames returns the ".go" words in the plain row that are not whole
// tab names.
func cutNames(row string, bufs []nvim.BufferInfo) []string {
	whole := map[string]bool{}
	for _, b := range bufs {
		whole[b.Path[strings.LastIndex(b.Path, "/")+1:]] = true
	}
	var bad []string
	for _, f := range strings.FieldsFunc(row, func(r rune) bool {
		return r == ' ' || r == '│' || r == '◀' || r == '▶' || r == '▎'
	}) {
		if strings.Contains(f, "go") && !whole[f] {
			bad = append(bad, f)
		}
	}
	return bad
}

// Spec: Given more tabs than fit in the tab bar
// When any tab is active, or the user scrolls the bar with the wheel
// Then only whole tabs show (no name is cut at either edge) and the active
// tab is fully visible.
func TestOverflowShowsWholeTabsOnly(t *testing.T) {
	bufs := manyBufs()
	for _, w := range []int{30, 41, 57, 80} {
		for _, b := range bufs {
			m := New()
			m.SetWidth(w)
			m.SetBuffers(bufs, b.ID)
			row := ansi.Strip(m.View())
			name := b.Path[strings.LastIndex(b.Path, "/")+1:]
			if !strings.Contains(row, name) {
				t.Errorf("w=%d active %s not visible: %q", w, name, row)
			}
			if bad := cutNames(row, bufs); len(bad) > 0 {
				t.Errorf("w=%d active %s: cut names %q in %q", w, name, bad, row)
			}
			for i := 0; i < 6; i++ {
				m, _ = m.HandleMouse(5, 0, 0, tea.MouseWheelDown)
				row = ansi.Strip(m.View())
				if bad := cutNames(row, bufs); len(bad) > 0 {
					t.Errorf("w=%d after wheel: cut names %q in %q", w, bad, row)
				}
			}
		}
	}
}

// Spec: Given one tab whose name is wider than the whole bar
// When it is the active tab
// Then its name shows from the start and ends with "…" (never cut on the
// left), and the row keeps the bar width.
func TestLongActiveTabTruncatesAtEnd(t *testing.T) {
	long := "a_really_long_file_name_for_testing.go"
	bufs := append(manyBufs()[:3], nvim.BufferInfo{ID: 99, Path: "/tmp/" + long})
	m := New()
	m.SetWidth(24)
	m.SetBuffers(bufs, 99)
	row := ansi.Strip(m.View())
	if ansi.StringWidth(row) != 24 {
		t.Fatalf("row width %d, want 24: %q", ansi.StringWidth(row), row)
	}
	if !strings.Contains(row, "a_really_long") || !strings.Contains(row, "…") {
		t.Fatalf("long name should show its start and end with …: %q", row)
	}
}
