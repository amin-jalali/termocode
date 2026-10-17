package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Spec (SV-13): a hidden Services view costs nothing. Before it is ever
// shown, docker is never called; after it hides, no services message
// arrives and no docker process runs for 5 s.
func TestServicesHiddenIdle(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	f := svcFake(t)
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": svcCompose}})
	d.Settle(500 * time.Millisecond)
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("docker ran before the view was shown: %q", calls)
	}
	d.Palette("services-focus")
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.PID("events.pid") > 0 }) {
		t.Fatal("no watcher while shown")
	}
	pid := f.PID("events.pid")
	d.Action("view:files")
	if !servicestest.WaitGone(pid, 3*time.Second) {
		t.Fatal("watcher alive after hide")
	}
	d.Settle(300 * time.Millisecond)
	from, _ := d.Updates()
	calls := len(f.Calls())
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		d.Settle(250 * time.Millisecond)
	}
	for _, u := range d.UpdatesSince(from) {
		if strings.Contains(u, "services") {
			t.Errorf("hidden view got %s", u)
		}
	}
	if n := len(f.Calls()); n != calls {
		t.Errorf("docker ran while hidden: %q", f.Calls()[calls:])
	}
}
