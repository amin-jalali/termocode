package confirm

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A Risky card puts the safe answer first, plain and focused: Enter on a
// fresh card picks it, Esc closes the card, and WithFocus can not move the
// focus to an action.
func TestRiskyCardSafeFirstAndFocused(t *testing.T) {
	m := Risky("Allow?", "body",
		Button{ID: "deny", Title: "Deny", Style: StyleDestructive},
		Button{ID: "allow", Title: "Allow", Style: StylePrimary},
		Button{ID: "always", Title: "Always allow"})
	b := m.Buttons()
	if len(b) != 3 || b[0].ID != "deny" || b[1].ID != "allow" || b[2].ID != "always" {
		t.Fatalf("buttons = %+v", b)
	}
	if b[0].Style != StyleDefault {
		t.Errorf("safe button style %q, want plain", b[0].Style)
	}
	if m.SafeID() != "deny" {
		t.Errorf("SafeID = %q", m.SafeID())
	}
	enter := func(m Model) string {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("Enter does nothing")
		}
		sel, ok := cmd().(SelectMsg)
		if !ok {
			t.Fatalf("Enter gives %T", cmd())
		}
		return sel.ID
	}
	if got := enter(m); got != "deny" {
		t.Errorf("Enter picks %q, want deny", got)
	}
	if got := enter(m.WithFocus(1)); got != "deny" {
		t.Errorf("WithFocus moved a Risky card's focus: Enter picks %q", got)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc does nothing")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Errorf("Esc gives %T, want CloseMsg", cmd())
	}
	// Right moves to the action; Enter then picks it.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := enter(m2); got != "allow" {
		t.Errorf("Right, Enter picks %q, want allow", got)
	}
	if New("x", "y", []Button{{ID: "ok"}}).SafeID() != "" {
		t.Error("a plain card has a SafeID")
	}
}
