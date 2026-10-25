package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A picker can carry one extra row key: it reports the highlighted row
// (ActionMsg), shows the key in the footer, and the footer hint is a button.
func TestRowActionKeyReportsHighlightedRow(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "a", Title: "First"}, {ID: "b", Title: "Second"}})
	m.SetSize(120, 40)
	m.SetRowAction("→", "details")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd == nil {
		t.Fatal("Right did nothing")
	}
	got, ok := cmd().(ActionMsg)
	if !ok || got.ID != "b" || got.Key != "→" {
		t.Fatalf("got %#v", cmd())
	}
	if !strings.Contains(ansi.Strip(m.Box()), "details") {
		t.Error("footer does not name the key")
	}
	// Without a row action Right stays silent.
	plain := NewItems(" P ", []Item{{ID: "a", Title: "First"}})
	plain.SetSize(120, 40)
	if _, c := plain.Update(tea.KeyMsg{Type: tea.KeyRight}); c != nil {
		t.Error("Right acted without SetRowAction")
	}
}
