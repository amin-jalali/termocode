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

// The row action key can be Space ("space"): Space reports the row
// instead of typing a blank into the search, and the footer names it.
func TestRowActionSpace(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "a", Title: "First"}, {ID: "b", Title: "Second"}})
	m.SetSize(120, 40)
	m.SetRowAction("space", "on/off")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("Space did nothing")
	}
	if got, ok := cmd().(ActionMsg); !ok || got.ID != "b" || got.Key != "space" {
		t.Fatalf("got %#v", cmd())
	}
	if m.input != "" {
		t.Errorf("Space typed into the search: %q", m.input)
	}
	if !strings.Contains(ansi.Strip(m.Box()), "on/off") {
		t.Error("footer does not name the key")
	}
}

// SetItems swaps the rows but keeps the search text and the highlighted
// row (a toggle redraws its row in place).
func TestSetItemsKeepsSearchAndCursor(t *testing.T) {
	m := NewItems(" P ", []Item{{ID: "a", Title: "alpha"}, {ID: "b", Title: "beta"}, {ID: "c", Title: "gamma"}})
	m.SetSize(120, 40)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.SetCursorID("c")
	m.SetItems([]Item{{ID: "a", Title: "alpha"}, {ID: "b", Title: "beta"}, {ID: "c", Title: "gamma [off]"}})
	if m.input != "a" {
		t.Errorf("search = %q", m.input)
	}
	if it, ok := m.Current(); !ok || it.ID != "c" || it.Title != "gamma [off]" {
		t.Errorf("current = %+v %v", it, ok)
	}
}
