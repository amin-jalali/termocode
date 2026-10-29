package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func headOf(t *testing.T, r *gitTestRepo) string {
	t.Helper()
	return strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
}

// G-UND: the plan for the last action (a commit) moves HEAD back to the
// commit before it, names both, and sees no local changes in the way.
func TestPlanUndoCommit(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Entry.Kind != ReflogCommit || p.Entry.Subject != "c3" || p.Target != h[1] || p.TargetSubject != "c2" {
		t.Errorf("plan %+v", p)
	}
	if p.From != h[2] || p.Branch != "main" || p.Switch != "" || len(p.Blocked) != 0 || p.Pushed != 0 || p.Staged {
		t.Errorf("plan %+v", p)
	}
	if p.TargetShort() != h[1][:7] {
		t.Errorf("short %q", p.TargetShort())
	}
}

// G-UND: a local change to a file the undo changes is in the way; a
// change to another file is not. Staged changes are noted.
func TestPlanUndoBlockedFiles(t *testing.T) {
	r, _ := rebaseRepo(t, 3)
	r.write(t, "f3.txt", "my edit\n")
	r.write(t, "f1.txt", "other edit\n")
	r.git(t, "add", "f1.txt")
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Blocked, ",") != "f3.txt" || !p.Staged {
		t.Errorf("blocked %q staged %v", p.Blocked, p.Staged)
	}
}

// G-UND: commits the undo drops from the branch that are on the upstream
// already are counted (they would need a force-push).
func TestPlanUndoPushed(t *testing.T) {
	r, _ := rebaseRepo(t, 2)
	remote := t.TempDir()
	r.git(t, "init", "-q", "--bare", remote)
	r.git(t, "remote", "add", "origin", remote)
	r.git(t, "push", "-q", "-u", "origin", "main")
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Upstream != "origin/main" || p.Pushed != 1 {
		t.Errorf("upstream %q pushed %d", p.Upstream, p.Pushed)
	}
}

// G-UND: undoing a checkout switches back to the branch left.
func TestPlanUndoCheckout(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "-b", "feat/x", h[0])
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Entry.Kind != ReflogCheckout || p.Switch != "main" || p.Target != h[1] || p.Branch != "feat/x" {
		t.Errorf("plan %+v", p)
	}
}

// G-UND: no undo during a stopped merge (or rebase …), and none before
// the first commit.
func TestPlanUndoRefuses(t *testing.T) {
	r := twoSidesRepo(t)
	gitFails(t, r, "merge", "side")
	var busy *BusyError
	if _, err := PlanUndo(r.dir); !errors.As(err, &busy) || busy.Op != "merge" {
		t.Errorf("merge in progress: %v", err)
	}
	one, _ := rebaseRepo(t, 1)
	if _, err := PlanUndo(one.dir); !errors.Is(err, ErrUndoFirstCommit) {
		t.Errorf("first commit: %v", err)
	}
	if _, err := PlanUndo(newGitTestRepo(t).dir); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("empty repo: %v", err)
	}
}

// G-UND: a reflog line picked from the list plans a move of HEAD to the
// commit that line left HEAD on.
func TestPlanMoveTo(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	log, err := Reflog(r.dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PlanMoveTo(r.dir, log[2]) // "commit 'c1'"
	if err != nil {
		t.Fatal(err)
	}
	if p.Target != h[0] || p.TargetSubject != "c1" || p.Switch != "" || p.From != h[2] {
		t.Errorf("plan %+v", p)
	}
}

// G-UND: RunUndo moves HEAD back with reset --keep: the undone commit's
// file goes, other local changes stay, the commit is still in the reflog.
func TestRunUndoCommit(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	r.write(t, "f1.txt", "my edit\n")
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunUndo(r.dir, p, UndoOptions{}); err != nil {
		t.Fatal(err)
	}
	if headOf(t, r) != h[1] {
		t.Errorf("HEAD not back at c2")
	}
	if fileExists(r, "f3.txt") || r.read(t, "f1.txt") != "my edit\n" {
		t.Errorf("work tree wrong: f3 %v f1 %q", fileExists(r, "f3.txt"), r.read(t, "f1.txt"))
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "HEAD@{1}")); got != h[2] {
		t.Errorf("c3 not in the reflog: %s", got)
	}
}

// G-UND: a local change in the way stops the undo with
// ErrUndoLocalChanges; nothing moves and the change stays.
func TestRunUndoRefusesLocalChanges(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	r.write(t, "f3.txt", "my edit\n")
	p, _ := PlanUndo(r.dir)
	if _, err := RunUndo(r.dir, p, UndoOptions{}); !errors.Is(err, ErrUndoLocalChanges) {
		t.Fatalf("err %v", err)
	}
	if headOf(t, r) != h[2] || r.read(t, "f3.txt") != "my edit\n" {
		t.Errorf("something moved")
	}
}

// G-UND: when HEAD moved after the plan was made, the undo refuses.
func TestRunUndoStale(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, _ := PlanUndo(r.dir)
	r.write(t, "f4.txt", "4\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "c4")
	if _, err := RunUndo(r.dir, p, UndoOptions{}); !errors.Is(err, ErrUndoStale) {
		t.Fatalf("err %v", err)
	}
	if headOf(t, r) == h[1] {
		t.Error("HEAD moved")
	}
}

func fileExists(r *gitTestRepo, name string) bool {
	_, err := os.Stat(filepath.Join(r.dir, name))
	return err == nil
}

// G-UND: Stash and Undo puts the changes in the way aside, moves HEAD and
// puts them back.
func TestRunUndoStash(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.write(t, "f1.txt", "top\nfile 1\n")
	r.git(t, "commit", "-q", "-am", "c3")
	r.write(t, "f1.txt", "top\nfile 1\nbottom\n")
	p, _ := PlanUndo(r.dir)
	if len(p.Blocked) != 1 {
		t.Fatalf("blocked %q", p.Blocked)
	}
	res, err := RunUndo(r.dir, p, UndoOptions{Stash: true})
	if err != nil {
		t.Fatal(err)
	}
	if headOf(t, r) != h[1] || r.read(t, "f1.txt") != "file 1\nbottom\n" {
		t.Errorf("HEAD %s f1 %q", headOf(t, r), r.read(t, "f1.txt"))
	}
	if !res.Stashed || res.StashKept || strings.TrimSpace(r.git(t, "stash", "list")) != "" {
		t.Errorf("result %+v, stash list %q", res, r.git(t, "stash", "list"))
	}
}

// G-UND: when the changes do not go back cleanly, the undo is done, the
// stash stays and the result says so.
func TestRunUndoStashConflictKeepsStash(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	r.write(t, "f3.txt", "my edit\n") // c3 adds f3; the undo removes it
	p, _ := PlanUndo(r.dir)
	res, err := RunUndo(r.dir, p, UndoOptions{Stash: true})
	if err != nil {
		t.Fatal(err)
	}
	if headOf(t, r) != h[1] {
		t.Errorf("HEAD not moved")
	}
	if !res.StashKept || res.StashRef != "stash@{0}" || !strings.Contains(r.git(t, "stash", "list"), "before undo") {
		t.Errorf("result %+v, stash list %q", res, r.git(t, "stash", "list"))
	}
}

// G-UND: undoing a checkout switches back to the branch left; the other
// branch stays where it is.
func TestRunUndoCheckout(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "-b", "feat/x", h[0])
	p, _ := PlanUndo(r.dir)
	if _, err := RunUndo(r.dir, p, UndoOptions{}); err != nil {
		t.Fatal(err)
	}
	if b := strings.TrimSpace(r.git(t, "branch", "--show-current")); b != "main" || headOf(t, r) != h[1] {
		t.Errorf("on %q at %s", b, headOf(t, r))
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "feat/x")); got != h[0] {
		t.Errorf("feat/x moved to %s", got)
	}
}

// G-UND: Redo puts HEAD back where the undo found it, for a reset and for
// a checkout.
func TestRunRedo(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, _ := PlanUndo(r.dir)
	res, err := RunUndo(r.dir, p, UndoOptions{})
	if err != nil || res.After != h[1] {
		t.Fatalf("undo %+v %v", res, err)
	}
	if err := RunRedo(r.dir, p, res.After); err != nil {
		t.Fatal(err)
	}
	if headOf(t, r) != h[2] || !fileExists(r, "f3.txt") {
		t.Errorf("not redone")
	}

	r.git(t, "checkout", "-q", "-b", "feat/x", h[0])
	p, _ = PlanUndo(r.dir)
	res, err = RunUndo(r.dir, p, UndoOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := RunRedo(r.dir, p, res.After); err != nil {
		t.Fatal(err)
	}
	if b := strings.TrimSpace(r.git(t, "branch", "--show-current")); b != "feat/x" || headOf(t, r) != h[0] {
		t.Errorf("on %q at %s", b, headOf(t, r))
	}
}

// G-UND: Redo refuses once HEAD moved after the undo.
func TestRunRedoStale(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, _ := PlanUndo(r.dir)
	res, _ := RunUndo(r.dir, p, UndoOptions{})
	r.write(t, "f9.txt", "9\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "c9")
	if err := RunRedo(r.dir, p, res.After); !errors.Is(err, ErrUndoStale) {
		t.Fatalf("err %v", err)
	}
	if headOf(t, r) == h[2] {
		t.Error("HEAD moved back to c3")
	}
}
