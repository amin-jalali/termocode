package menu

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Hovering a row moves the highlight (cursor) onto it; motion outside the
// panel leaves the highlight where it was.
func TestHoverMovesHighlight(t *testing.T) {
	items := []Item{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}, {ID: "c", Title: "Gamma"}}
	m := New(items, 5, 5)
	m.SetScreenSize(80, 24)
	x, y := m.clampedAnchor()

	// Row i renders at y + 1 (top border) + i (no header on this menu).
	m2, _ := m.HandleMouse(tea.MouseMsg{Type: tea.MouseMotion, X: x + 2, Y: y + 1 + 2})
	if m2.cursor != 2 {
		t.Fatalf("hover over row 2: cursor=%d, want 2", m2.cursor)
	}

	// Motion outside the bordered panel must not move the highlight.
	m3, _ := m2.HandleMouse(tea.MouseMsg{Type: tea.MouseMotion, X: 0, Y: 0})
	if m3.cursor != 2 {
		t.Fatalf("hover outside panel: cursor=%d, want 2 (unchanged)", m3.cursor)
	}
}

// Hovering a separator row is a no-op (the highlight stays on a real item).
func TestHoverSkipsSeparator(t *testing.T) {
	items := []Item{{ID: "a", Title: "Alpha"}, {Sep: true}, {ID: "b", Title: "Beta"}}
	m := New(items, 5, 5)
	m.SetScreenSize(80, 24)
	x, y := m.clampedAnchor()
	// Separator is item index 1 → row y+1+1.
	m2, _ := m.HandleMouse(tea.MouseMsg{Type: tea.MouseMotion, X: x + 2, Y: y + 1 + 1})
	if m2.cursor == 1 {
		t.Fatalf("hover landed on separator row (cursor=1), want it skipped")
	}
}
