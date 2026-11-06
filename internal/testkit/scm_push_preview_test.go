package testkit

import (
	"strings"
	"testing"
)

// Source Control: push and pull options (GITX-12).

// Spec: Push… lists the commits that go out on a card where Cancel is
// ready: Enter pushes nothing; the Push button pushes them.
func TestSCMPushPreviewCancelFirst(t *testing.T) {
	var bare string
	d, r := scmDriver(t, func(r *repo) {
		bare = r.addOrigin()
		r.write("b.txt", "b\n")
		r.commit("second change")
	})
	remoteTip := func() string {
		o := &repo{t: r.t, dir: bare}
		return o.git("log", "-1", "--format=%s", "main")
	}
	open := func() {
		d.Palette("git-push-preview")
		if !until(d, func() bool { return strings.Contains(text(d), "Push 1 commit to origin/main?") }) {
			t.Fatalf("no card:%s", dump(d))
		}
		if !strings.Contains(text(d), "second change") {
			t.Fatalf("the card does not list the commit:%s", dump(d))
		}
	}
	open()
	d.Key("enter") // Cancel
	d.Settle(0)
	if remoteTip() != "init" {
		t.Fatal("Enter pushed")
	}
	open()
	clickTarget(d, "confirm:push")
	if !until(d, func() bool { return remoteTip() == "second change" }) {
		t.Errorf("Push did not push:%s", dump(d))
	}
}
