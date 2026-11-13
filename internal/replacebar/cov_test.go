package replacebar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Messages that are not keys change nothing and start nothing.
func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	m := New()
	got, cmd := m.Update(tea.WindowSizeMsg{Width: 80})
	if cmd != nil || got != m {
		t.Fatal("a non-key message changed the bar")
	}
}

// Keys with no meaning here (arrows left/right, F-keys) are ignored.
func TestUpdateIgnoresOtherKeys(t *testing.T) {
	m := New()
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyF5, tea.KeyCtrlA} {
		got, cmd := m.Update(tea.KeyMsg{Type: k})
		if cmd != nil || got != m {
			t.Fatalf("key %v changed the bar", k)
		}
	}
}

// Down from Replace, and Up from Find, move focus the other way round.
func TestFocusWrapsBothWays(t *testing.T) {
	m := New()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.focus != fieldFind {
		t.Fatal("second Down should come back to Find")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.focus != fieldReplace {
		t.Fatal("Up from Find should go to Replace")
	}
}

// Enter with Alt held replaces all, like Alt+Enter spelled as a string.
func TestAltEnterReplacesAll(t *testing.T) {
	m := New()
	m.find, m.replace = "a", "b"
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got, ok := cmd().(ReplaceAllMsg); !ok || got.Find != "a" || got.Replace != "b" {
		t.Fatalf("got %#v", cmd())
	}
}

// A very narrow bar keeps a minimum input width; a long value shows its end.
func TestRenderRowNarrowAndLongValue(t *testing.T) {
	m := New()
	m.SetWidth(10)
	m.find = strings.Repeat("x", 60)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Find") || !strings.Contains(out, "Replace") {
		t.Fatalf("rows missing: %q", out)
	}
	m.SetWidth(200)
	m.find = strings.Repeat("y", 300)
	if !strings.Contains(ansi.Strip(m.View()), "yyyy") {
		t.Fatal("the end of a long value is not shown")
	}
}
