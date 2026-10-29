package prompt

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMultilineRenameNames(t *testing.T) {
	m := New("Rename", "", "a.txt\nb.txt")
	m.SetMultiline()
	key := func(k tea.KeyType) { m, _ = m.Update(tea.KeyMsg{Type: k}) }
	key(tea.KeyCtrlK)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c.txt")})
	key(tea.KeyDown)
	key(tea.KeyHome)
	key(tea.KeyCtrlK)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d.txt")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := cmd().(SubmitMsg).Value; got != "c.txt\nd.txt" {
		t.Fatal(got)
	}
	key(tea.KeyEnter)
	if m.Value() != "c.txt\nd.txt\n" {
		t.Fatal(m.Value())
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatal("escape")
	}
}

func TestMultilinePasteKeepsEmptyLines(t *testing.T) {
	m := New("Rename", "", "")
	m.SetMultiline()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\n\na.txt\n")})
	if m.Value() != "\n\na.txt\n" {
		t.Fatal(m.Value())
	}
}
