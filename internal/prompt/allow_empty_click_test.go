package prompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// On an optional step ("empty to finish") a click on "Enter submit" does
// what the Enter key does: it submits the empty value.
func TestEnterHintSubmitsEmptyWhenAllowed(t *testing.T) {
	m := New("Add servers", "Host (empty to finish):", "")
	m.SetAllowEmpty(true)
	m.SetSize(120, 40)
	bx, by := m.Origin()
	lines := strings.Split(m.Box(), "\n")
	foot := ansi.Strip(lines[len(lines)-2])
	i := strings.Index(foot, "Enter")
	if i < 0 {
		t.Fatalf("no Enter hint in %q", foot)
	}
	_, cmd := m.HandleMouse(bx+ansi.StringWidth(foot[:i]), by+len(lines)-2, tea.MouseActionPress, tea.MouseButtonLeft)
	if cmd == nil {
		t.Fatal("click on Enter submit did nothing, the Enter key submits")
	}
	if s, ok := cmd().(SubmitMsg); !ok || s.Value != "" {
		t.Errorf("click sent %#v, want SubmitMsg{\"\"}", cmd())
	}
}
