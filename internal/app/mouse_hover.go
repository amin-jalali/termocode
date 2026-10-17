package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseHover is the "mouse_hover" setting, read at start (SetMouseHover):
// false keeps the terminal on button-only tracking (1002) for good, so no
// hover labels and no row highlight under a still pointer.
var mouseHover = true

// SetMouseHover sets the mouse_hover setting before the UI starts.
func SetMouseHover(on bool) { mouseHover = on }

// MouseHoverEnabled reports the mouse_hover setting (cmd/termocode picks
// the first tracking mode with it).
func MouseHoverEnabled() bool { return mouseHover }

// ConfigMouseHover returns "mouse_hover" from config.json ("" = auto).
func ConfigMouseHover() string { return loadSettings().MouseHover }

// ResolveMouseHover turns the setting into on/off: only "off" turns hover
// off. auto is on everywhere: split mouse reports are joined and motion
// bursts are merged in the input reader (third_party/bubbletea
// input_hold.go), and frames are paced over SSH.
func ResolveMouseHover(v string) bool {
	return !strings.EqualFold(strings.TrimSpace(v), "off")
}

// wantAllMotion: all-motion tracking (1003) only while something can use
// hover. Not while a text input is open (no hover there, and it kept the
// old fragment leak away), not while the terminal window is in the
// background (its pointer motion would only cost updates), not with
// mouse_hover off.
func (m Model) wantAllMotion() bool {
	return mouseHover && !m.termBlurred && !m.anyTextInputOpen()
}

// syncMouseMode switches the terminal's tracking mode when wantAllMotion
// changed; nil when it did not.
func (m *Model) syncMouseMode() tea.Cmd {
	want := m.wantAllMotion()
	if want == m.mouseAllMotion {
		return nil
	}
	m.mouseAllMotion = want
	if want {
		return tea.EnableMouseAllMotion
	}
	return tea.EnableMouseCellMotion
}
