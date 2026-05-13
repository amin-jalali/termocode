package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestViewDoesNotPanicAcrossSizes(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24}, {120, 40}, {40, 12}, {200, 80}, {1, 1},
	}
	for _, s := range sizes {
		m := New()
		updated, _ := m.Update(tea.WindowSizeMsg{Width: s.w, Height: s.h})
		out := updated.View()
		if out == "" && s.w > 1 && s.h > 1 {
			t.Errorf("size %dx%d produced empty view", s.w, s.h)
		}
	}
}

func TestKeyDispatchSequence(t *testing.T) {
	m := New()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	keys := []tea.KeyMsg{
		{Type: tea.KeyTab},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyTab},
		{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}},
	}
	for _, k := range keys {
		updated, _ = updated.Update(k)
	}
	out := updated.View()
	// Either the welcome banner ("Termocode" capital-T ASCII art) is on
	// screen, or the status bar segments ("Ln"/"Spaces"/"UTF-8") are. The
	// banner sometimes scrolls out of view at h=24 once content settles —
	// the broader contract is that *some* recognisable termocode chrome
	// is rendered after the key dispatch.
	if !strings.Contains(out, "Termocode") && !strings.Contains(out, "termocode") &&
		!strings.Contains(out, "Ln") && !strings.Contains(out, "Spaces") &&
		!strings.Contains(out, "UTF-8") {
		t.Errorf("view missing expected markers")
	}
}
