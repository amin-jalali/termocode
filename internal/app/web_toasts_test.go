package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-12): the toasts topic mirrors the laptop's toasts: the snap
// lists them, a new one is a push, a keyed update a push with the same
// id, a gone one a dismiss; the deltas replay to the newest snapshot.
func TestToastsPushDismiss(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.toast, _ = m.toast.Push(toast.Info, "Saved")
	c := webTestSub(t, &m, "toasts")
	got := webTestNow(t, c)
	var s webproto.ToastsSnap
	webTestData(t, got[0], &s)
	if len(s.Toasts) != 1 || s.Toasts[0].Title != "Saved" || s.Toasts[0].Severity != "info" {
		t.Fatalf("snap %+v", s)
	}
	m.toast, _ = m.toast.PushKeyed("clone", toast.Warn, "Cloning", "10%")
	m.webAfterUpdate(nil)
	m.toast, _ = m.toast.PushKeyed("clone", toast.Warn, "Cloning", "60%")
	m.webAfterUpdate(nil)
	m.toast = m.toast.DismissID(s.Toasts[0].ID)
	m.toast, _ = m.toast.PushDetail(toast.Errr, "Build failed", "exit 2")
	m.webAfterUpdate(nil)
	m.webAfterUpdate(nil) // nothing changed: nothing sent
	var ops []string
	for _, f := range webTestNow(t, c) {
		var d webproto.ToastsDelta
		webTestData(t, f, &d)
		op := d.Op
		if d.Toast != nil {
			op += " " + d.Toast.Severity + " " + d.Toast.Title + " " + d.Toast.Detail
		}
		ops = append(ops, op)
	}
	want := []string{"push warn Cloning 10%", "push warn Cloning 60%", "dismiss", "push error Build failed exit 2"}
	if len(ops) != len(want) {
		t.Fatalf("deltas %q, want %q", ops, want)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("deltas %q, want %q", ops, want)
		}
	}
}
