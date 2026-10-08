package toast

import "testing"

func TestPushKeyedUpdatesInPlace(t *testing.T) {
	m := New()
	m, _ = m.PushKeyed("k", Info, "Cloning", "10%")
	m, _ = m.Push(Info, "other")
	m, _ = m.PushKeyed("k", Info, "Cloning", "50%")
	if len(m.toasts) != 2 {
		t.Fatalf("keyed push should not stack: %d toasts", len(m.toasts))
	}
	if m.toasts[0].Detail != "50%" {
		t.Errorf("detail not updated: %q", m.toasts[0].Detail)
	}
	m = m.Dismiss("k")
	if len(m.toasts) != 1 || m.toasts[0].Title != "other" {
		t.Errorf("dismiss removed the wrong toast: %+v", m.toasts)
	}
}
