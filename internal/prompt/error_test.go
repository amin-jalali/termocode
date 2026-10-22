package prompt

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
)

// SetError shows a refusal under the input; typing or deleting clears it.
func TestSetErrorShowsAndClearsOnEdit(t *testing.T) {
	m := New("T", "label", "abc")
	m.SetSize(80, 24)
	m.SetError("Not a valid reference. Try again.")
	if m.Error() == "" || !strings.Contains(ansi.Strip(m.View()), "Not a valid reference") {
		t.Fatalf("refusal not shown:\n%s", ansi.Strip(m.View()))
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.Error() != "" || strings.Contains(ansi.Strip(m.View()), "Not a valid reference") {
		t.Fatal("typing must clear the refusal")
	}
	m.SetError("again")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Error() != "" {
		t.Fatal("backspace must clear the refusal")
	}
	m.SetError("keep")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Error() != "keep" {
		t.Fatal("Enter alone must not clear it")
	}
}
