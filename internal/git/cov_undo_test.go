package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// An undo that would overwrite local changes is refused; with Stash the
// changes are put away first and brought back after, and a pop that
// conflicts leaves the stash where the owner can find it.
func TestUndoWithLocalChangesInTheWay(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "x.txt", "a\n")
	r.git(t, "add", "x.txt")
	r.git(t, "commit", "-qm", "c1")
	c1 := headOf(t, r)
	r.write(t, "x.txt", "b\n")
	r.git(t, "commit", "-qam", "c2")
	c2 := headOf(t, r)
	r.write(t, "x.txt", "c\n")

	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Blocked) != 1 || p.Blocked[0] != "x.txt" {
		t.Fatalf("Blocked = %v", p.Blocked)
	}
	if _, err := RunUndo(r.dir, p, UndoOptions{}); !errors.Is(err, ErrUndoLocalChanges) {
		t.Fatalf("without Stash = %v", err)
	}
	if headOf(t, r) != c2 {
		t.Fatal("a refused undo moved HEAD")
	}
	res, err := RunUndo(r.dir, p, UndoOptions{Stash: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stashed || res.After != c1 || !res.StashKept || res.StashRef != "stash@{0}" || res.PopError == "" {
		t.Errorf("result = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(r.dir, "x.txt")); len(got) == 0 {
		t.Error("x.txt is empty")
	}
}

func TestRunUndoStaleHead(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	r.git(t, "reset", "-q", "--hard", h[1])
	if _, err := RunUndo(r.dir, p, UndoOptions{}); !errors.Is(err, ErrUndoStale) {
		t.Errorf("HEAD moved since the plan = %v", err)
	}
}

func TestUndoACheckoutAndRedoIt(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "-b", "topic")
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Switch != "main" || p.Branch != "topic" {
		t.Fatalf("plan = %+v", p)
	}
	res, err := RunUndo(r.dir, p, UndoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.git(t, "branch", "--show-current"); got != "main\n" {
		t.Errorf("after undo on %q", got)
	}
	if err := RunRedo(r.dir, p, res.After); err != nil {
		t.Fatal(err)
	}
	if got := r.git(t, "branch", "--show-current"); got != "topic\n" {
		t.Errorf("after redo on %q", got)
	}
	if headOf(t, r) != h[1] {
		t.Error("topic should be at c2")
	}
	// A detached plan goes back to its branch on redo.
	d := UndoPlan{Detach: true, Branch: "topic", From: h[1], Target: h[0]}
	if _, err := RunUndo(r.dir, d, UndoOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := RunRedo(r.dir, d, h[0]); err != nil {
		t.Errorf("redo of a detached undo = %v", err)
	}
}
