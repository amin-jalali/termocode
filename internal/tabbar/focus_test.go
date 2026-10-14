package tabbar

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TestActiveMarkerFollowsFocus: the active tab's ▎ is drawn in the focus
// accent only while the editor has focus. When focus is in the sidebar,
// the terminal or the AI chat, the marker is dimmed, so the bright accent
// always points at the pane that takes the keys. The text stays the same.
func TestActiveMarkerFollowsFocus(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	focused := m.View()
	m.SetFocused(false)
	blurred := m.View()
	if focused == blurred {
		t.Fatal("active tab looks the same with and without focus")
	}
	if ansi.Strip(focused) != ansi.Strip(blurred) {
		t.Errorf("text changed with focus:\n%q\n%q", ansi.Strip(focused), ansi.Strip(blurred))
	}
	m.SetFocused(true)
	if m.View() != focused {
		t.Error("focus back: tab bar not restored")
	}
}
