package testkit

import (
	"strings"
	"testing"
)

// Source Control: Git: Merge Branch… and the stopped-merge banner (GITX-1).

// mergeSide makes branch "side" change f.txt; with conflict main changes
// it too.
func mergeSide(conflict bool) func(r *repo) {
	return func(r *repo) {
		r.write("f.txt", "base\n")
		r.commit("base")
		r.git("checkout", "-q", "-b", "side")
		r.write("f.txt", "side\n")
		r.commit("side work")
		r.git("checkout", "-q", "main")
		if conflict {
			r.write("f.txt", "main\n")
			r.commit("main work")
		}
	}
}

// pickMergeBranch runs Git: Merge Branch… and picks side.
func pickMergeBranch(t *testing.T, d *Driver) {
	t.Helper()
	d.Palette("git-merge-branch")
	d.Type("side")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "Merge side into main?") }) {
		t.Fatalf("no merge card:%s", dump(d))
	}
}

// Spec: Merge Branch… asks on a Cancel-first card (Enter merges
// nothing); Merge fast-forwards main to side.
func TestSCMMergeBranchFastForward(t *testing.T) {
	d, r := scmDriver(t, mergeSide(false))
	before := r.git("rev-parse", "HEAD")
	pickMergeBranch(t, d)
	if !strings.Contains(text(d), "1 commit of side comes into main") {
		t.Errorf("card does not count the commits:%s", dump(d))
	}
	d.Key("enter") // Cancel
	d.Settle(0)
	if r.git("rev-parse", "HEAD") != before {
		t.Fatal("Enter merged")
	}
	pickMergeBranch(t, d)
	clickTarget(d, "confirm:merge")
	if !until(d, func() bool { return strings.Contains(text(d), "Merged side into main") }) {
		t.Fatalf("no merged toast:%s", dump(d))
	}
	if r.git("rev-parse", "HEAD") != r.git("rev-parse", "side") {
		t.Error("main did not move to side")
	}
}

// Spec: a conflicting merge stops with a banner; its card (Close first)
// keeps the merge on Enter; Abort puts main back and the banner goes.
func TestSCMMergeBranchConflictBannerAbort(t *testing.T) {
	d, r := scmDriver(t, mergeSide(true))
	head := r.git("rev-parse", "HEAD")
	pickMergeBranch(t, d)
	clickTarget(d, "confirm:merge")
	if !until(d, func() bool { return rowWith(d, "Merging · 1 conflict") >= 0 && inSection(d, "CONFLICTS", "f.txt") }) {
		t.Fatalf("no banner:%s", dump(d))
	}
	scmClickText(d, "Merging")
	if !until(d, func() bool { return strings.Contains(text(d), "Merge in progress") }) {
		t.Fatalf("no card:%s", dump(d))
	}
	d.Key("enter") // Close
	d.Settle(0)
	if rowWith(d, "Merging") < 0 {
		t.Fatal("Enter ended the merge")
	}
	scmClickText(d, "Merging")
	until(d, func() bool { return strings.Contains(text(d), "Merge in progress") })
	clickTarget(d, "confirm:op-abort")
	if !until(d, func() bool { return strings.Contains(text(d), "Merge aborted") && rowWith(d, "Merging") < 0 }) {
		t.Fatalf("abort did not end the merge:%s", dump(d))
	}
	if r.git("rev-parse", "HEAD") != head || r.porcelain() != "" {
		t.Error("main not put back")
	}
}
