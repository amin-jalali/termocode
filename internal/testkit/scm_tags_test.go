package testkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Source Control: tags and force push (GITX-11, GITX-14).

// Spec: deleting a tag asks on a Cancel-first card (Enter deletes
// nothing); Delete removes it and the toast names the commit that brings
// it back.
func TestSCMTagDeleteCancelFirst(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) { r.git("tag", "v1") })
	tip := r.git("rev-parse", "--short", "v1")
	pick := func() {
		d.Palette("git-tag-delete")
		d.Type("v1")
		d.Key("enter")
		if !until(d, func() bool { return strings.Contains(text(d), "Delete tag v1?") }) {
			t.Fatalf("no card:%s", dump(d))
		}
	}
	pick()
	d.Key("enter") // Cancel
	d.Settle(0)
	if r.git("tag") != "v1" {
		t.Fatal("Enter deleted the tag")
	}
	pick()
	clickTarget(d, "confirm:delete")
	if !until(d, func() bool { return strings.Contains(text(d), "Deleted tag v1") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
	if r.git("tag") != "" || !strings.Contains(text(d), tip) {
		t.Errorf("tag left or tip %s not shown:%s", tip, dump(d))
	}
}

// Spec: Force Push (with Lease)… on a branch with no remote says so and
// pushes nothing; no card opens.
func TestSCMForcePushNeedsRemote(t *testing.T) {
	d, _ := scmDriver(t, nil)
	out := ansi.Strip(d.Palette("git-push-force"))
	if !strings.Contains(out, "not on a remote yet") {
		t.Fatalf("no note:%s", out)
	}
	if strings.Contains(out, "Force push main?") {
		t.Error("a card opened without a remote")
	}
}
