package confirm

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Hovering a button moves the highlight (cursor) onto it without emitting a
// SelectMsg (that's reserved for a real click).
func TestHoverMovesButtonHighlight(t *testing.T) {
	m := New("Discard changes?", "This cannot be undone.", sampleButtons())
	m.SetSize(80, 24)
	_, rects := m.computeRects()
	if len(rects) < 2 {
		t.Fatalf("need at least 2 buttons, got %d", len(rects))
	}

	br := rects[1]
	m2, cmd, _ := m.HandleMouse(br.x, br.y, tea.MouseActionMotion, tea.MouseButtonNone)
	if m2.cursor != 1 {
		t.Fatalf("hover over button 1: cursor=%d, want 1", m2.cursor)
	}
	if cmd != nil {
		t.Fatalf("hover must not emit a command (no select on hover)")
	}
}
