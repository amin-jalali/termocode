package prompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMaskedPromptHidesValue(t *testing.T) {
	m := New("Token", "Token:", "")
	m.SetMasked(true)
	m.SetSize(100, 30)
	m, _ = m.Update(runesEvent("s3cret"))
	if v := m.View(); strings.Contains(v, "s3cret") || !strings.Contains(v, "••••••") {
		t.Errorf("masked prompt leaked or lost the value:\n%s", v)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if msg, ok := cmd().(SubmitMsg); !ok || msg.Value != "s3cret" {
		t.Errorf("submit should carry the real value, got %+v", msg)
	}
}
