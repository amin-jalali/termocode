package testkit

import (
	"strings"
	"testing"
)

// Source Control: one-step history edits from the GRAPH (GITX-17).

// Spec: D on a GRAPH commit asks "Drop commit …?" on a card where Cancel
// is ready: Enter keeps the commit; the Drop button removes it and keeps
// the newer commits.
func TestSCMDropCommitCancelFirst(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("b.txt", "b\n")
		r.commit("unwanted change")
		r.write("c.txt", "c\n")
		r.commit("keep this")
	})
	short := r.git("rev-parse", "--short", "HEAD~1")
	open := func() {
		openCommitView(t, d, r, "unwanted change", "HEAD~1")
		d.Key("esc")
		d.Key("D")
		if !until(d, func() bool { return strings.Contains(text(d), "Drop commit "+short+"?") }) {
			t.Fatalf("no card:%s", dump(d))
		}
	}
	open()
	d.Key("enter") // Cancel
	d.Settle(0)
	if got := r.git("log", "--format=%s"); got != "keep this\nunwanted change\ninit" {
		t.Fatalf("Enter changed the history:\n%s", got)
	}
	open()
	clickTarget(d, "confirm:drop")
	if !until(d, func() bool { return r.git("log", "--format=%s") == "keep this\ninit" }) {
		t.Errorf("not dropped:\n%s%s", r.git("log", "--format=%s"), dump(d))
	}
	if r.exists("b.txt") || !r.exists("c.txt") {
		t.Error("the files do not match the new history")
	}
}
