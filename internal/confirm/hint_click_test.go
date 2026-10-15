package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec (2026-10-15, "mouse & keyboard should work in every menu"): the
// footer hints of the confirm dialog are buttons: "Enter select" picks the
// highlighted button, "Esc cancel" closes.
func TestConfirmFooterHintsClickable(t *testing.T) {
	m := New("Discard?", "Discard all changes?", []Button{{ID: "cancel", Title: "Cancel"}, {ID: "ok", Title: "Discard", Style: "danger"}})
	m.SetSize(120, 40)
	m.cursor = 1
	box, _ := m.computeRects()
	lines := strings.Split(m.Box(), "\n")
	at := func(label string) (int, int) {
		for y, l := range lines {
			p := ansi.Strip(l)
			if i := strings.LastIndex(p, label); i >= 0 {
				return box.x + ansi.StringWidth(p[:i]) + 1, box.y + y
			}
		}
		t.Fatalf("%q not found", label)
		return 0, 0
	}
	x, y := at("Enter")
	_, cmd, _ := m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("Enter hint did nothing")
	}
	if s, ok := cmd().(SelectMsg); !ok || s.ID != "ok" {
		t.Fatalf("Enter hint: %#v", cmd())
	}
	x, y = at("cancel")
	_, cmd, _ = m.HandleMouse(x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("Esc hint did nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf("Esc hint: %#v", cmd())
	}
}
