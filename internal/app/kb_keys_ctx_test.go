package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// KB-1: D removes (asks first); making a context the default moved to u.
func TestKB1DOnAContextAsksToRemoveIt(t *testing.T) {
	m, f := ctxSession(t)
	selectContext(t, m.services, "prod")
	if cmd := press(m, "D"); cmd != nil {
		t.Fatal("D ran something before the card")
	}
	if !m.confirmOpen || !strings.Contains(strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " "), "context rm prod") {
		t.Fatalf("D on a context did not ask to remove it: open=%v", m.confirmOpen)
	}
	if f.Count("context rm") != 0 {
		t.Fatalf("remove ran before the card was answered: %q", f.Calls())
	}
}

func TestKB1UMakesAContextTheDefault(t *testing.T) {
	m, _ := ctxSession(t)
	selectContext(t, m.services, "prod")
	press(m, "u")
	card := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " ")
	if !m.confirmOpen || !strings.Contains(card, "context use prod") {
		t.Fatalf("u did not open the make-default card: open=%v %s", m.confirmOpen, card)
	}
}

func TestKB1XNoLongerRemovesAContext(t *testing.T) {
	m, _ := ctxSession(t)
	selectContext(t, m.services, "prod")
	press(m, "x")
	if m.confirmOpen {
		t.Fatal("x on a context still opens a card: x means stop, D means remove")
	}
}
