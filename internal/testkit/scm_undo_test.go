//go:build unix

package testkit

import (
	"strings"
	"testing"
)

// Undo with the reflog (G-UND).

// Spec: u in Source Control asks first ("Undo the last git action?") and
// Enter alone picks Cancel. Undo moves HEAD back (GRAPH drops the commit);
// "Git: Redo Undone Git Action" puts it back.
func TestSCMUndoKeyAndRedo(t *testing.T) {
	d, r := scmDriver(t, rebaseRepo)
	head := r.git("rev-parse", "HEAD")
	openUndo := func() {
		t.Helper()
		scmClickText(d, "c4")
		d.Key("u")
		if !until(d, func() bool { return strings.Contains(text(d), "Undo the last git action?") }) {
			t.Fatalf("no undo card:%s", dump(d))
		}
	}
	openUndo()
	if s := text(d); !strings.Contains(s, "Last action: commit 'c4'") {
		t.Errorf("card lacks the action:%s", dump(d))
	}
	d.Key("enter") // Cancel is focused
	if !until(d, func() bool { return !strings.Contains(text(d), "Undo the last git action?") }) {
		t.Fatalf("card still open:%s", dump(d))
	}
	if r.git("rev-parse", "HEAD") != head {
		t.Fatal("Enter undid the commit")
	}
	openUndo()
	clickTarget(d, "confirm:undo")
	if !until(d, func() bool { return strings.Contains(text(d), "Undone: commit 'c4'") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if !until(d, func() bool { return !inSection(d, "GRAPH", "c4") }) {
		t.Errorf("GRAPH still shows c4:%s", dump(d))
	}
	d.Palette("git-redo")
	if !until(d, func() bool { return strings.Contains(text(d), "Redone: commit 'c4'") }) {
		t.Fatalf("no redo toast:%s", dump(d))
	}
	if r.git("rev-parse", "HEAD") != head {
		t.Error("redo did not put c4 back")
	}
}

// Spec: "Git: Reflog…" lists the actions; Enter on one asks "Move HEAD
// back to here?" and Move HEAD resets the branch there.
func TestSCMReflogMoveHere(t *testing.T) {
	d, r := scmDriver(t, rebaseRepo)
	c2 := r.git("rev-parse", "HEAD~2")
	d.Palette("git-reflog")
	if !until(d, func() bool { return strings.Contains(text(d), "Reflog: move HEAD back to which point?") }) {
		t.Fatalf("no reflog picker:%s", dump(d))
	}
	d.Type("'c2'")
	d.Key("enter")
	if !until(d, func() bool { return strings.Contains(text(d), "Move HEAD back to here?") }) {
		t.Fatalf("no card:%s", dump(d))
	}
	clickTarget(d, "confirm:undo")
	if !until(d, func() bool { return strings.Contains(text(d), "HEAD moved back to") }) {
		t.Fatalf("no done toast:%s", dump(d))
	}
	if r.git("rev-parse", "HEAD") != c2 {
		t.Errorf("HEAD not at c2")
	}
}
