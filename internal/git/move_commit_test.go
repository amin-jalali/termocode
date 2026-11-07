package git

import (
	"strings"
	"testing"
)

func moveTrim(t *testing.T, r *gitTestRepo, args ...string) string {
	t.Helper()
	return strings.TrimSpace(r.git(t, args...))
}

// GITX-39 (J13): the last commit moves to a new branch; the current
// branch drops it and the uncommitted changes stay.
func TestMoveLastCommitToNewBranch(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "base")
	r.commitFile(t, "b.txt", "feat: b")
	r.write(t, "a.txt", "edited\n")
	in, err := LastCommitForMove(r.dir)
	if err != nil || in.Branch != "main" || in.Subject != "feat: b" || in.Pushed {
		t.Fatalf("info %+v %v", in, err)
	}
	if err := MoveLastCommit(r.dir, in.Hash, "feat/b", true, false); err != nil {
		t.Fatal(err)
	}
	if got := moveTrim(t, r, "log", "--format=%s", "main"); got != "base" {
		t.Errorf("main %q", got)
	}
	if got := moveTrim(t, r, "rev-parse", "feat/b"); got != in.Hash {
		t.Errorf("feat/b at %s, want %s", got, in.Hash)
	}
	if got := moveTrim(t, r, "status", "--porcelain"); got != "M a.txt" {
		t.Errorf("status %q", got)
	}
	if err := MoveLastCommit(r.dir, in.Hash, "feat/b", true, false); err == nil {
		t.Error("a stale hash was moved")
	}
}

// The last commit goes on top of an existing branch with its message and
// author; nothing is checked out.
func TestMoveLastCommitToExistingBranch(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "base")
	r.git(t, "branch", "other")
	r.git(t, "checkout", "-q", "other")
	r.commitFile(t, "o.txt", "other work")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "b.txt", "b\n")
	r.git(t, "add", "b.txt")
	// --author: TestMain's GIT_AUTHOR_* env wins over -c user.name.
	r.git(t, "commit", "-q", "--author=Ann <ann@example.com>", "-m", "feat: b\n\nbody line")
	in, _ := LastCommitForMove(r.dir)
	if err := MoveLastCommit(r.dir, in.Hash, "other", false, false); err != nil {
		t.Fatal(err)
	}
	if got := moveTrim(t, r, "log", "--format=%s|%an", "other"); got != "feat: b|Ann\nother work|Test\nbase|Test" {
		t.Errorf("other:\n%s", got)
	}
	if got := moveTrim(t, r, "log", "-1", "--format=%B", "other"); got != "feat: b\n\nbody line" {
		t.Errorf("message %q", got)
	}
	if got := moveTrim(t, r, "ls-tree", "--name-only", "other"); got != "a.txt\nb.txt\no.txt" {
		t.Errorf("tree %q", got)
	}
	if got := moveTrim(t, r, "log", "--format=%s", "main"); got != "base" {
		t.Errorf("main %q", got)
	}
	if moveTrim(t, r, "symbolic-ref", "--short", "HEAD") != "main" {
		t.Error("the branch was switched")
	}
}

// A pushed commit is only copied (keep): the current branch keeps it.
func TestMoveLastCommitPushedKeeps(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "base")
	bare := t.TempDir()
	r.git(t, "init", "-q", "--bare", bare)
	r.git(t, "remote", "add", "origin", bare)
	r.commitFile(t, "b.txt", "feat: b")
	r.git(t, "push", "-q", "-u", "origin", "main")
	in, _ := LastCommitForMove(r.dir)
	if !in.Pushed {
		t.Fatal("a pushed commit is not marked pushed")
	}
	if err := MoveLastCommit(r.dir, in.Hash, "copy", true, true); err != nil {
		t.Fatal(err)
	}
	if moveTrim(t, r, "rev-parse", "main") != in.Hash || moveTrim(t, r, "rev-parse", "copy") != in.Hash {
		t.Error("keep moved the commit")
	}
}

// A commit that does not apply on the target, or local changes in its
// files, change nothing.
func TestMoveLastCommitRefusals(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitFile(t, "a.txt", "base")
	if _, err := LastCommitForMove(r.dir); err == nil {
		t.Error("the first commit is offered")
	}
	r.git(t, "branch", "other")
	r.git(t, "checkout", "-q", "other")
	r.write(t, "a.txt", "theirs\n")
	r.git(t, "commit", "-q", "-am", "other a")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "a.txt", "ours\n")
	r.git(t, "commit", "-q", "-am", "main a")
	in, _ := LastCommitForMove(r.dir)
	otherTip := moveTrim(t, r, "rev-parse", "other")
	if err := MoveLastCommit(r.dir, in.Hash, "other", false, false); err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Errorf("conflict err %v", err)
	}
	if moveTrim(t, r, "rev-parse", "other") != otherTip || moveTrim(t, r, "rev-parse", "main") != in.Hash {
		t.Error("a failed move changed a branch")
	}
	r.write(t, "a.txt", "dirty\n")
	if err := MoveLastCommit(r.dir, in.Hash, "new", true, false); err == nil {
		t.Error("local changes in the commit's file were lost or mixed")
	}
	if moveTrim(t, r, "rev-parse", "main") != in.Hash || strings.TrimSpace(r.git(t, "branch", "--list", "new")) != "" {
		t.Error("a failed move changed a branch")
	}
	if err := MoveLastCommit(r.dir, in.Hash, "main", false, false); err == nil {
		t.Error("moved onto its own branch")
	}
	if err := MoveLastCommit(r.dir, in.Hash, "-x", true, false); err == nil {
		t.Error("a flag-like name was taken")
	}
	r.git(t, "checkout", "-q", "--detach")
	if _, err := LastCommitForMove(r.dir); err == nil {
		t.Error("a detached HEAD is offered")
	}
}
