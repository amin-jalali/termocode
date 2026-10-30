package toast

import "testing"

// Spec (WEB-12): Live lists the toasts on show, oldest first, as copies;
// a keyed push updates its toast in place (same ID) and a toast sliding
// out is not live any more.
func TestLiveListsShownToasts(t *testing.T) {
	var m Model
	m, _ = m.Push(Info, "Saved")
	m, _ = m.PushKeyed("clone", Warn, "Cloning", "10%")
	m, _ = m.PushKeyed("clone", Warn, "Cloning", "60%")
	live := m.Live()
	if len(live) != 2 || live[0].Title != "Saved" || live[1].Detail != "60%" || live[1].ID != 2 {
		t.Fatalf("live %+v", live)
	}
	live[0].Title = "changed"
	if m.Live()[0].Title != "Saved" {
		t.Fatal("Live must return a copy")
	}
	m.toasts[0].leaving = true
	if live := m.Live(); len(live) != 1 || live[0].Title != "Cloning" {
		t.Fatalf("a leaving toast is not live: %+v", live)
	}
}
