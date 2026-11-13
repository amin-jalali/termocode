package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastCommitForMoveRefusals(t *testing.T) {
	// A detached HEAD.
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "--detach", h[1])
	if _, err := LastCommitForMove(r.dir); err == nil || !strings.Contains(err.Error(), "not on a branch") {
		t.Errorf("detached = %v", err)
	}
	r.git(t, "checkout", "-q", "main")
	// A rebase in progress.
	if err := os.MkdirAll(filepath.Join(r.dir, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LastCommitForMove(r.dir); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Errorf("rebase = %v", err)
	}
	// No commits yet.
	e := newGitTestRepo(t)
	if _, err := LastCommitForMove(e.dir); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("empty repo = %v", err)
	}
	// The first commit.
	f, _ := rebaseRepo(t, 1)
	if _, err := LastCommitForMove(f.dir); err == nil || !strings.Contains(err.Error(), "first commit") {
		t.Errorf("first commit = %v", err)
	}
	// A merge commit.
	m, _ := rebaseRepo(t, 1)
	m.git(t, "checkout", "-q", "-b", "side")
	m.write(t, "s.txt", "s\n")
	m.git(t, "add", "s.txt")
	m.git(t, "commit", "-qm", "side")
	m.git(t, "checkout", "-q", "main")
	m.write(t, "m.txt", "m\n")
	m.git(t, "add", "m.txt")
	m.git(t, "commit", "-qm", "main work")
	m.git(t, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	if _, err := LastCommitForMove(m.dir); err == nil || !strings.Contains(err.Error(), "merge commit") {
		t.Errorf("merge = %v", err)
	}
}

func TestMoveLastCommitMoreRefusals(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	cases := []struct {
		name, hash, target, want string
		create                   bool
	}{
		{"stale hash", h[0], "x", "changed", true},
		{"empty target", h[2], "", "not a branch name", true},
		{"bad name", h[2], "a..b", "not a branch name", true},
		{"same branch", h[2], "main", "already", true},
		{"missing branch", h[2], "nope", "no branch nope", false},
	}
	for _, c := range cases {
		err := MoveLastCommit(r.dir, c.hash, c.target, c.create, false)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// A branch that exists already cannot be created.
	r.git(t, "branch", "old", h[0])
	if err := MoveLastCommit(r.dir, h[2], "old", true, false); err == nil || !strings.Contains(err.Error(), "exists already") {
		t.Errorf("create over an existing branch = %v", err)
	}
	// A branch checked out in another worktree.
	wt := filepath.Join(t.TempDir(), "wt")
	r.git(t, "worktree", "add", "-q", wt, "old")
	if err := MoveLastCommit(r.dir, h[2], "old", false, false); err == nil || !strings.Contains(err.Error(), "checked out in") {
		t.Errorf("checked-out target = %v", err)
	}
	// A branch that has the commit already.
	r.git(t, "branch", "same", h[2])
	if err := MoveLastCommit(r.dir, h[2], "same", false, false); err == nil || !strings.Contains(err.Error(), "has this commit") {
		t.Errorf("target holding it = %v", err)
	}
	// Nothing moved by all the refusals.
	if headOf(t, r) != h[2] {
		t.Error("a refused move changed HEAD")
	}
}

func TestMoveLastCommitOntoExistingBranch(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	r.git(t, "branch", "other", h[0])
	if err := MoveLastCommit(r.dir, h[2], "other", false, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s", "other")); got != "c3" {
		t.Errorf("other's tip = %q", got)
	}
	if headOf(t, r) != h[1] {
		t.Error("main should have dropped the commit")
	}
}

func TestWorktreeRemoveAndRestoreGuards(t *testing.T) {
	r := committedRepo(t)
	outside := filepath.Join(t.TempDir(), "x")
	if err := RemoveWorktree(r.dir, outside, false); err == nil || !strings.Contains(err.Error(), "not a worktree") {
		t.Errorf("unknown path = %v", err)
	}
	if err := RemoveWorktree(r.dir, r.dir, true); err == nil || !strings.Contains(err.Error(), "main checkout") {
		t.Errorf("main = %v", err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	r.git(t, "worktree", "add", "-q", "-b", "wb", wt)
	r.git(t, "worktree", "lock", "--reason", "usb", wt)
	if err := RemoveWorktree(r.dir, wt, false); err == nil || !strings.Contains(err.Error(), "usb") {
		t.Errorf("locked with reason = %v", err)
	}
	r.git(t, "worktree", "unlock", wt)
	r.git(t, "worktree", "lock", wt)
	if err := RemoveWorktree(r.dir, wt, false); err == nil || !strings.Contains(err.Error(), "unlock") {
		t.Errorf("locked = %v", err)
	}
	r.git(t, "worktree", "unlock", wt)
	if err := os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorktree(r.dir, wt, false); err == nil {
		t.Error("a dirty worktree is kept without discard")
	}
	if err := RemoveWorktree(r.dir, wt, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("the folder should be gone")
	}
	// Restore.
	if err := RestoreWorktree(r.dir, wt, "gone-branch"); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("missing branch = %v", err)
	}
	if err := RestoreWorktree(r.dir, "rel/path", "wb"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative path = %v", err)
	}
	if err := RestoreWorktree(r.dir, r.dir, "wb"); err == nil || !strings.Contains(err.Error(), "there already") {
		t.Errorf("existing folder = %v", err)
	}
	// main is checked out in the main worktree: git refuses and the
	// folders made for it are removed again.
	deep := filepath.Join(t.TempDir(), "a", "b", "wt")
	if err := RestoreWorktree(r.dir, deep, "main"); err == nil {
		t.Error("restoring a checked-out branch: want error")
	}
	if _, err := os.Stat(filepath.Join(deep, "..", "..")); err == nil {
		t.Error("the folders made for the failed restore should be removed")
	}
	if err := RestoreWorktree(r.dir, wt, "wb"); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultBranchAndBranchUnmerged(t *testing.T) {
	r := committedRepo(t)
	if got := DefaultBranch(r.dir); got != "main" {
		t.Errorf("DefaultBranch = %q", got)
	}
	r.git(t, "update-ref", "refs/remotes/origin/main", "HEAD")
	r.git(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	if got := DefaultBranch(r.dir); got != "main" {
		t.Errorf("DefaultBranch with origin/HEAD = %q", got)
	}
	if DefaultBranch(newGitTestRepo(t).dir) != "" {
		t.Error("no main or master: empty")
	}
	if _, err := BranchUnmerged(r.dir, "-x", "main"); err == nil {
		t.Error("an option as branch must be refused")
	}
	if _, err := BranchUnmerged(r.dir, "main", "-x"); err == nil {
		t.Error("an option as base must be refused")
	}
	if _, err := BranchUnmerged(r.dir, "nope", "main"); err == nil {
		t.Error("unknown branch: want error")
	}
}
