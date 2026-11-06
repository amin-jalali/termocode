package testkit

import (
	"strings"
	"testing"
)

// Source Control: Git: Delete Branch… (GITX-2).

// Spec: deleting an unmerged branch asks on a Cancel-first card (Enter
// deletes nothing), then on a second Cancel-first card before the force
// delete; the toast names the tip that brings it back.
func TestSCMBranchDeleteUnmergedForce(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.git("checkout", "-q", "-b", "wip")
		r.write("w.txt", "w\n")
		r.commit("wip work")
		r.git("checkout", "-q", "main")
	})
	tip := r.git("rev-parse", "--short", "wip")
	pick := func() {
		d.Palette("git-branch-delete")
		d.Type("wip")
		d.Key("enter")
		if !until(d, func() bool { return strings.Contains(text(d), "Delete branch wip?") }) {
			t.Fatalf("no card:%s", dump(d))
		}
	}
	pick()
	d.Key("enter") // Cancel
	d.Settle(0)
	if !strings.Contains(r.git("branch"), "wip") {
		t.Fatal("Enter deleted the branch")
	}
	pick()
	clickTarget(d, "confirm:delete")
	if !until(d, func() bool { return strings.Contains(text(d), "wip is not merged") }) {
		t.Fatalf("no force card:%s", dump(d))
	}
	d.Key("enter") // Cancel again
	d.Settle(0)
	if !strings.Contains(r.git("branch"), "wip") {
		t.Fatal("Enter force-deleted the branch")
	}
	pick()
	clickTarget(d, "confirm:delete")
	until(d, func() bool { return strings.Contains(text(d), "wip is not merged") })
	clickTarget(d, "confirm:force-delete")
	if !until(d, func() bool { return strings.Contains(text(d), "Deleted branch wip") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
	if strings.Contains(r.git("branch"), "wip") || !strings.Contains(text(d), tip) {
		t.Errorf("branch left or tip %s not shown:%s", tip, dump(d))
	}
}
