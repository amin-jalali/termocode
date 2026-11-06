package git

import (
	"strings"
	"testing"
)

// mergeRepo is main with a.txt and a branch "side" that changes b.txt in
// two commits (main has not moved: a fast-forward is possible).
func mergeRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\ntwo\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "b.txt", "b1\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "side 1")
	r.write(t, "b.txt", "b2\n")
	r.git(t, "commit", "-q", "-am", "side 2")
	r.git(t, "checkout", "-q", "main")
	return r
}

// conflictOnA makes main and side both change a.txt's first line.
func conflictOnA(t *testing.T, r *gitTestRepo) {
	t.Helper()
	r.git(t, "checkout", "-q", "side")
	r.write(t, "a.txt", "SIDE\ntwo\n")
	r.git(t, "commit", "-q", "-am", "side a")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "a.txt", "MAIN\ntwo\n")
	r.git(t, "commit", "-q", "-am", "main a")
}

func parents(t *testing.T, r *gitTestRepo) int {
	t.Helper()
	return len(strings.Fields(r.git(t, "log", "-1", "--format=%P")))
}

// GITX-1: the card counts the commits that come in; none when the branch
// is merged already.
func TestMergeIncoming(t *testing.T) {
	r := mergeRepo(t)
	if n, err := MergeIncoming(r.dir, "side"); err != nil || n != 2 {
		t.Fatalf("incoming %d %v", n, err)
	}
	r.git(t, "merge", "-q", "side")
	if n, _ := MergeIncoming(r.dir, "side"); n != 0 {
		t.Errorf("after merge %d", n)
	}
	if _, err := MergeIncoming(r.dir, "nope"); err == nil {
		t.Error("no error for an unknown branch")
	}
}

// GITX-1: a plain merge fast-forwards when it can; no-ff always makes a
// merge commit.
func TestMergeBranchFastForwardAndNoFF(t *testing.T) {
	r := mergeRepo(t)
	if c, err := MergeBranch(r.dir, "side", false); err != nil || len(c) != 0 {
		t.Fatalf("merge: %v %v", c, err)
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != strings.TrimSpace(r.git(t, "rev-parse", "side")) || parents(t, r) != 1 {
		t.Error("not a fast-forward")
	}

	r2 := mergeRepo(t)
	if _, err := MergeBranch(r2.dir, "side", true); err != nil {
		t.Fatal(err)
	}
	if parents(t, r2) != 2 {
		t.Error("no merge commit with --no-ff")
	}
	if msg := r2.git(t, "log", "-1", "--format=%s"); !strings.Contains(msg, "side") {
		t.Errorf("message %q", msg)
	}
}

// GITX-1: a conflicting merge stops with the conflicted files; Continue
// refuses while markers are left and commits once they are fixed.
func TestMergeBranchConflictContinue(t *testing.T) {
	r := mergeRepo(t)
	conflictOnA(t, r)
	c, err := MergeBranch(r.dir, "side", false)
	if err == nil || strings.Join(c, ",") != "a.txt" {
		t.Fatalf("conflicts %v err %v", c, err)
	}
	if op := StoppedOp(r.dir); op != "merge" {
		t.Fatalf("stopped op %q", op)
	}
	if err := OpContinue(r.dir, "merge"); err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("continue with markers: %v", err)
	}
	r.write(t, "a.txt", "BOTH\ntwo\n")
	if err := OpContinue(r.dir, "merge"); err != nil {
		t.Fatal(err)
	}
	if StoppedOp(r.dir) != "" || parents(t, r) != 2 {
		t.Error("merge not committed")
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "" {
		t.Errorf("status %q", got)
	}
}

// GITX-1: Abort puts the branch back as it was.
func TestMergeAbort(t *testing.T) {
	r := mergeRepo(t)
	conflictOnA(t, r)
	before := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	_, _ = MergeBranch(r.dir, "side", false)
	if err := OpAbort(r.dir, "merge"); err != nil {
		t.Fatal(err)
	}
	if StoppedOp(r.dir) != "" || strings.TrimSpace(r.git(t, "rev-parse", "HEAD")) != before {
		t.Error("abort did not restore")
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "" {
		t.Errorf("status %q", got)
	}
}

// GITX-1 (I11): a stopped cherry-pick goes on and aborts the same way; a
// rebase is not reported (it has its own banner).
func TestStoppedCherryPick(t *testing.T) {
	r := mergeRepo(t)
	conflictOnA(t, r)
	hash := strings.TrimSpace(r.git(t, "rev-parse", "side"))
	if _, err := CherryPick(r.dir, hash); err == nil {
		t.Fatal("cherry-pick did not stop")
	}
	if op := StoppedOp(r.dir); op != "cherry-pick" {
		t.Fatalf("op %q", op)
	}
	r.write(t, "a.txt", "PICKED\ntwo\n")
	if err := OpContinue(r.dir, "cherry-pick"); err != nil {
		t.Fatal(err)
	}
	if StoppedOp(r.dir) != "" {
		t.Error("still stopped")
	}
	if err := OpContinue(r.dir, "rebase"); err == nil {
		t.Error("rebase accepted")
	}
}

// GITX-1: local changes the merge would overwrite stop it before it
// starts; nothing is left half-way.
func TestMergeBranchDirtyTree(t *testing.T) {
	r := mergeRepo(t)
	r.write(t, "b.txt", "mine\n")
	if _, err := MergeBranch(r.dir, "side", false); err == nil {
		t.Fatal("merged over local changes")
	}
	if StoppedOp(r.dir) != "" {
		t.Error("left a merge in progress")
	}
}
