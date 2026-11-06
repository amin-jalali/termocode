package testkit

import (
	"strings"
	"testing"
)

// Source Control: reset from the GRAPH (GITX-15).

// Spec: Reset Current Branch to Here… on an older commit, Hard: the card
// is Cancel-first (Enter resets nothing); after Reset the branch name is
// typed, the changes go to a stash and HEAD to a backup ref, then the
// branch is at the commit.
func TestSCMResetHardFromGraph(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\n// two\n")
		r.commit("two")
	})
	first := r.git("rev-list", "--max-parents=0", "HEAD")
	r.write("main.go", "package main\n\n// unsaved\n")
	open := func() {
		y := mustRow(d, "init")
		d.RightClick(colOf(d, y, "init"), y)
		if !until(d, func() bool { return strings.Contains(text(d), "Reset Current Branch to Here…") }) {
			t.Fatalf("no menu item:%s", dump(d))
		}
		clickScreen(d, "Reset Current Branch to Here…")
		if !until(d, func() bool { return strings.Contains(text(d), "Reset main to") }) {
			t.Fatalf("no mode picker:%s", dump(d))
		}
		d.Type("Hard")
		d.Key("enter")
		if !until(d, func() bool { return strings.Contains(text(d), "1 commit leave") }) {
			t.Fatalf("no card:%s", dump(d))
		}
	}
	open()
	d.Key("enter") // Cancel
	d.Settle(0)
	if r.git("rev-parse", "HEAD") == first {
		t.Fatal("Enter reset the branch")
	}
	open()
	clickTarget(d, "confirm:reset")
	d.Type("main")
	d.Key("enter")
	if !until(d, func() bool { return r.git("rev-parse", "HEAD") == first }) {
		t.Fatalf("not reset:%s", dump(d))
	}
	if !strings.Contains(r.git("stash", "list"), "before reset --hard") || !strings.Contains(r.git("for-each-ref", "refs/termocode/backup"), "commit") {
		t.Error("the changes or HEAD were not saved first")
	}
}
