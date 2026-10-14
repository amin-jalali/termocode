package toast

import (
	"testing"
	"time"
)

type showMsg struct{ path string }

// A clickable toast carries its message on the card, lives longer than a
// plain one, and goes away by ID.
func TestPushClick(t *testing.T) {
	m := New()
	m, _ = m.Push(Info, "plain")
	m, _ = m.PushClick(Info, "Saved to ~/Downloads/a.txt", "Show in folder", showMsg{"/d/a.txt"})
	cards := m.Cards(40)
	if len(cards) != 2 || cards[0].Click != nil {
		t.Fatalf("cards %+v", cards)
	}
	c := cards[1]
	if got, ok := c.Click.(showMsg); !ok || got.path != "/d/a.txt" {
		t.Fatalf("click = %#v", c.Click)
	}
	if c.ID == 0 {
		t.Fatal("no ID on the card")
	}
	if life := time.Until(m.toasts[1].ExpiresAt); life <= defaultLifetime {
		t.Errorf("lifetime %v is not longer than %v", life, defaultLifetime)
	}
	if cc := m.CompactCards(30, 1); len(cc) != 1 || cc[0].Click == nil {
		t.Errorf("compact card lost the click: %+v", cc)
	}
	m = m.DismissID(c.ID)
	if len(m.toasts) != 1 || m.toasts[0].Title != "plain" {
		t.Errorf("DismissID left %+v", m.toasts)
	}
}
