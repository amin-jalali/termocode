package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A title longer than the box is cut with "…" on its one row. Before, it
// wrapped onto a second row, which pushed every list row down by one, so
// a click selected the row above the one under the pointer.
func TestLongTitleStaysOnOneRow(t *testing.T) {
	for _, w := range []int{40, 80, 200} {
		m := NewItems(strings.Repeat("A very long title ", 8), manyItems(5))
		m.SetSize(w, 30)
		_, boxH := m.BoxSize()
		lines := strings.Split(m.Box(), "\n")
		if len(lines) != boxH {
			t.Fatalf("width %d: box has %d lines, BoxSize says %d", w, len(lines), boxH)
		}
		if head := ansi.Strip(lines[1]); !strings.Contains(head, "…") || !strings.Contains(head, "5 matches") {
			t.Errorf("width %d: title row %q, want a cut title and the count", w, head)
		}
		r := m.RowRects()[3]
		if got := ansi.Strip(lines[r.Y-m.originY()]); !strings.Contains(got, "item 3") {
			t.Errorf("width %d: row rect 3 points at %q", w, got)
		}
		_, cmd := m.HandleMouse(r.X+4, r.Y, tea.MouseActionPress, tea.MouseButtonLeft)
		if cmd == nil {
			t.Fatalf("width %d: click on item 3 did nothing", w)
		}
		if sel, ok := cmd().(SelectMsg); !ok || sel.ID != "3" {
			t.Errorf("width %d: click on item 3 selected %#v", w, cmd())
		}
	}
}

// originY is the box's top row on screen.
func (m Model) originY() int { _, y, _, _ := m.boxRect(); return y }
