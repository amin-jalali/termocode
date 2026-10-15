package testkit

import (
	"strings"
	"testing"
)

// Revert and cherry-pick from the commit view (scm-graph design §3.5,
// task V4): always through the confirm overlay.

// clickTarget clicks the centre of the hit-map target id.
func clickTarget(d *Driver, id string) {
	d.T.Helper()
	for _, h := range d.Model().HitTargets() {
		if h.ID == id {
			d.Click(h.X+h.W/2, h.Y+h.H/2)
			return
		}
	}
	d.T.Fatalf("no target %q:%s", id, dump(d))
}

// pickFromCard opens subject's commit, presses button and waits for the
// confirm with title.
func pickFromCard(t *testing.T, d *Driver, r *repo, subject, rev, button, title string) {
	t.Helper()
	openCommitView(t, d, r, subject, rev)
	clickScreen(d, " "+button+" ")
	if !until(d, func() bool { return strings.Contains(text(d), title) }) {
		t.Fatalf("no %q confirm:%s", title, dump(d))
	}
}

// Spec: Revert… asks first; Revert makes a new "Revert …" commit, the
// GRAPH shows it and a toast says so.
func TestSCMRevertConfirm(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	pickFromCard(t, d, r, "after merge", "HEAD", "Revert…", "Revert commit?")
	if !strings.Contains(text(d), "after merge") {
		t.Errorf("confirm does not name the commit:%s", dump(d))
	}
	clickTarget(d, "confirm:revert")
	if !until(d, func() bool { return inSection(d, "GRAPH", `Revert "chore:`) }) {
		t.Fatalf("no revert commit in GRAPH:%s", dump(d))
	}
	if s := r.git("log", "-1", "--format=%s"); s != `Revert "chore: after merge"` {
		t.Errorf("HEAD is %q", s)
	}
	if !until(d, func() bool { return strings.Contains(text(d), "Reverted ") }) {
		t.Errorf("no toast:%s", dump(d))
	}
}

// Spec: Cancel in the confirm changes nothing.
func TestSCMRevertCancel(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	head := r.git("rev-parse", "HEAD")
	pickFromCard(t, d, r, "after merge", "HEAD", "Revert…", "Revert commit?")
	clickTarget(d, "confirm:cancel")
	d.Settle(0)
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved to %s", got)
	}
}

// Spec: Cherry-pick… copies a commit of another branch onto HEAD.
func TestSCMCherryPickFromBranch(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.git("checkout", "-q", "-b", "side")
		r.write("side.txt", "from side\n")
		r.commit("side work")
		r.git("checkout", "-q", "main")
		r.write("main.txt", "on main\n")
		r.commit("main work")
	})
	// GRAPH lists HEAD's history only; open the side commit by its SHA
	// (the dev hook the log picker would use).
	d.Action("commit:" + r.git("rev-parse", "side"))
	short := r.git("rev-parse", "--short", "side")
	if !until(d, func() bool { return strings.Contains(text(d), "Commit "+short) }) {
		t.Fatalf("side commit not shown:%s", dump(d))
	}
	clickScreen(d, " Cherry-pick… ")
	if !until(d, func() bool { return strings.Contains(text(d), "Cherry-pick commit?") }) {
		t.Fatalf("no confirm:%s", dump(d))
	}
	clickTarget(d, "confirm:cherry-pick")
	if !until(d, func() bool { return inSection(d, "GRAPH", "side work") }) {
		t.Fatalf("picked commit not in GRAPH:%s", dump(d))
	}
	if !r.exists("side.txt") {
		t.Error("side.txt not in the working tree")
	}
}

// Spec: a revert that conflicts shows CONFLICTS and a toast with the
// count.
func TestSCMRevertConflictShowsConflicts(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("f.txt", "one\n")
		r.commit("one")
		r.write("f.txt", "two\n")
		r.commit("two")
		r.write("f.txt", "three\n")
		r.commit("three")
	})
	pickFromCard(t, d, r, "two", "HEAD~1", "Revert…", "Revert commit?")
	clickTarget(d, "confirm:revert")
	if !until(d, func() bool { return strings.Contains(text(d), "Revert stopped: 1 conflict") }) {
		t.Fatalf("no conflict toast:%s", dump(d))
	}
	if !until(d, func() bool { return section(d, "CONFLICTS") != nil && inSection(d, "CONFLICTS", "f.txt") }) {
		t.Errorf("CONFLICTS does not list f.txt:%s", dump(d))
	}
}
