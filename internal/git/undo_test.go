package git

import (
	"errors"
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
