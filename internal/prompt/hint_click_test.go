package prompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec (2026-10-15): the prompt's footer hints are buttons: "Enter
// submit" submits the text, "Esc cancel" closes; a click outside does
// nothing.
func TestPromptFooterHintsClickable(t *testing.T) {
	m := New("Rename", "New name:", "notes.txt")
	m.SetSize(120, 40)
	x0, y0 := m.Origin()
	lines := strings.Split(m.Box(), "\n")
	at := func(label string) (int, int) {
		for y, l := range lines {
			p := ansi.Strip(l)
			if i := strings.LastIndex(p, label); i >= 0 {
				return x0 + ansi.StringWidth(p[:i]) + 1, y0 + y
			}
		}
		t.Fatalf("%q not found", label)
		return 0, 0
	}
	x, y := at("Enter")
	_, cmd := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("Enter hint did nothing")
	}
	if s, ok := cmd().(SubmitMsg); !ok || s.Value != "notes.txt" {
		t.Fatalf("Enter hint: %#v", cmd())
	}
	x, y = at("cancel")
	if _, cmd = m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft); cmd == nil {
		t.Fatal("Esc hint did nothing")
	} else if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("Esc hint: %#v", cmd())
	}
	if _, cmd = m.HandleMouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("a click outside did something")
	}
}
