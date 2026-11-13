package importview

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPanelTitleAndInit(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	if m.Title() == "" || m.Init() != nil {
		t.Fatal("title / init")
	}
}

// ? opens the key list; other messages and unknown keys do nothing.
func TestPanelHelpKeyAndIgnoredInput(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 42)
	if _, cmd := m.Update(key("?")); cmd == nil || cmd() != (KeysMsg{}) {
		t.Fatal("? did not ask for the keys")
	}
	if got, cmd := m.Update(tea.WindowSizeMsg{}); cmd != nil || got.cursor != m.cursor {
		t.Fatal("non-key message")
	}
	if got, cmd := m.Update(key("z")); cmd != nil || got.cursor != m.cursor {
		t.Fatal("unknown key")
	}
}

// Page and end keys move the cursor, [ goes to the previous section.
func TestPanelPagingAndSectionJumps(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 16)
	m, _ = m.Update(key("pgdown"))
	down := m.cursor
	if down == 0 {
		t.Fatal("PgDn did not move")
	}
	m, _ = m.Update(key("up"))
	if m.cursor != down-1 {
		t.Fatalf("up: %d -> %d", down, m.cursor)
	}
	m, _ = m.Update(key("pgup"))
	m, _ = m.Update(key("end"))
	if m.cursor != len(m.lines)-1 {
		t.Fatalf("end: %d of %d", m.cursor, len(m.lines))
	}
	m, _ = m.Update(key("["))
	if m.lines[m.cursor].row != -1 {
		t.Fatal("[ did not land on a section line")
	}
}

// Wheel up scrolls back; other buttons and releases do nothing; the
// All / None links select and clear.
func TestPanelMouseWheelUpAndLinks(t *testing.T) {
	m := newPanel(t, vscodeSrc, 150, 16)
	ox, oy := m.Origin()
	m, _ = m.HandleMouse(ox+5, oy+6, tea.MouseActionPress, tea.MouseButtonWheelDown)
	m, _ = m.HandleMouse(ox+5, oy+6, tea.MouseActionPress, tea.MouseButtonWheelDown)
	s := m.scroll
	m, _ = m.HandleMouse(ox+5, oy+6, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.scroll >= s {
		t.Fatalf("wheel up: %d -> %d", s, m.scroll)
	}
	if _, cmd := m.HandleMouse(ox+5, oy+6, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil {
		t.Fatal("right click did something")
	}
	if _, cmd := m.HandleMouse(ox+5, oy+6, tea.MouseActionRelease, tea.MouseButtonLeft); cmd != nil {
		t.Fatal("release did something")
	}
	m.SelectNone()
	if cmd := m.click("all"); cmd != nil || m.Counts().Total() == 0 {
		t.Fatal("All link")
	}
	if cmd := m.click("none"); cmd != nil || m.Counts().Total() != 0 {
		t.Fatal("None link")
	}
}
