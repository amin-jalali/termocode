package app

import (
	"os"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

// TerminalChangedMsg says that a different terminal now shows the UI: a
// client attached to the persistent session. cmd/termocode has already
// copied the new terminal's variables (TERM, COLORTERM, LANG …) into our
// environment; Graphics is the new terminal's graphics support and Gen
// the number of that attach. A tea.ClearScreen follows, so every line
// (and image) is drawn again.
type TerminalChangedMsg struct {
	Graphics termimg.Caps
	Gen      int
}

// applyTerminalChange re-resolves everything that depends on the terminal:
// the colour profile, the graphics protocol and cell size, icons and
// animations. Render caches are keyed by profile, protocol and icon set,
// so the next View draws for the new terminal.
func (m *Model) applyTerminalChange(msg TerminalChangedMsg) {
	lipgloss.SetColorProfile(theme.DetectProfile(os.Getenv))
	termCaps = msg.Graphics
	ApplyDisplaySettings(ConfigDisplay())
	m.termGen = msg.Gen
	if m.w > 0 && m.h > 0 {
		m.applyLayout()
	}
}

// markAttach puts session.AttachedSeq at the start of the frame once we
// draw for an attached terminal: the session server sends that terminal
// nothing from before it. It is zero-width, and the server removes it.
func (m Model) markAttach(frame string) string {
	if m.termGen <= 0 {
		return frame
	}
	return session.AttachedSeq(m.termGen) + frame
}
