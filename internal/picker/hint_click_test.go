package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec (bug report 2026-10-15, "mouse does not work in the menus"): the
// footer hints are buttons too: a click on "Enter select" selects the
// highlighted row, a click on "Esc close" closes the picker.
func TestFooterHintsAreClickable(t *testing.T) {
	m := NewItems(" AI Chat History ", []Item{{ID: "a", Title: "First chat"}, {ID: "b", Title: "Second chat"}})
	m.SetSize(120, 40)
	m.cursor = 1
	find := func(label string) (int, int) {
		bx, by, _, _ := m.boxRect()
		lines := strings.Split(m.Box(), "\n")
		for y, l := range lines {
			plain := ansi.Strip(l)
			if i := strings.Index(plain, label); i >= 0 {
				return bx + ansi.StringWidth(plain[:i]) + 1, by + y
			}
		}
		t.Fatalf("%q not in the box", label)
		return 0, 0
	}
	x, y := find("Enter")
	_, cmd := m.HandleMouse(x, y, mousePress(), mouseLeft())
	if cmd == nil {
		t.Fatal("click on Enter select did nothing")
	}
	if sel, ok := cmd().(SelectMsg); !ok || sel.ID != "b" {
		t.Fatalf("Enter hint: got %#v, want SelectMsg{ID: b}", cmd())
	}
	x, y = find("close")
	_, cmd = m.HandleMouse(x, y, mousePress(), mouseLeft())
	if cmd == nil {
		t.Fatal("click on Esc close did nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("Esc hint: got %#v, want CloseMsg", cmd())
	}
}

func mousePress() tea.MouseAction { return tea.MouseActionPress }
func mouseLeft() tea.MouseButton  { return tea.MouseButtonLeft }
