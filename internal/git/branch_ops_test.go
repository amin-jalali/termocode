package git

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// branchRepo is main with one commit and a branch "done" merged into it
// plus a branch "wip" with one commit main does not have.
func branchRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	r.git(t, "tag", "v1")
	r.git(t, "branch", "done")
	r.git(t, "checkout", "-q", "-b", "wip")
	r.write(t, "w.txt", "w\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "wip work")
	r.git(t, "checkout", "-q", "main")
	return r
}

func branchList(t *testing.T, r *gitTestRepo) string {
	t.Helper()
	b, err := Branches(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(b, ",")
}

// GITX-2: rename keeps the commits; a taken name is an error.
func TestRenameBranch(t *testing.T) {
	r := branchRepo(t)
	if err := RenameBranch(r.dir, "wip", "feature"); err != nil {
		t.Fatal(err)
	}
	if got := branchList(t, r); got != "done,feature,main" {
		t.Errorf("branches %s", got)
	}
	if err := RenameBranch(r.dir, "feature", "main"); err == nil {
		t.Error("renamed onto an existing branch")
	}
	if err := RenameBranch(r.dir, "done", "bad name"); err == nil {
		t.Error("accepted a bad name")
	}
}

// GITX-2: a safe delete takes a merged branch; an unmerged one is
// ErrBranchNotMerged and stays, until a force delete.
func TestDeleteBranchSafeThenForce(t *testing.T) {
	r := branchRepo(t)
	if err := DeleteBranch(r.dir, "done", false); err != nil {
		t.Fatal(err)
	}
	tip := BranchTip(r.dir, "wip")
	if len(tip) < 7 {
		t.Fatalf("tip %q", tip)
	}
	if err := DeleteBranch(r.dir, "wip", false); !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("unmerged delete: %v", err)
	}
	if got := branchList(t, r); got != "main,wip" {
		t.Errorf("branches %s", got)
	}
	if err := DeleteBranch(r.dir, "wip", true); err != nil {
		t.Fatal(err)
	}
	if got := branchList(t, r); got != "main" {
		t.Errorf("branches %s", got)
	}
	// The tip named before the delete brings the branch back.
	r.git(t, "branch", "wip", tip)
	if err := DeleteBranch(r.dir, "main", true); err == nil {
		t.Error("deleted the current branch")
	}
}

// GITX-2: a branch can start at any commit or tag, and the app switches
// to it.
func TestCreateBranchAt(t *testing.T) {
	r := branchRepo(t)
	if err := CreateBranchAt(r.dir, "from-tag", "v1"); err != nil {
		t.Fatal(err)
	}
	if cur := strings.TrimSpace(r.git(t, "branch", "--show-current")); cur != "from-tag" {
		t.Errorf("current %q", cur)
	}
	wip := strings.TrimSpace(r.git(t, "rev-parse", "wip"))
	if err := CreateBranchAt(r.dir, "from-hash", wip[:9]); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); got != wip {
		t.Errorf("head %s want %s", got, wip)
	}
	if err := CreateBranchAt(r.dir, "x", "no-such-ref"); err == nil {
		t.Error("no error for a bad start")
	}
}

// GITX-2 (K11): a commit can be checked out on its own (detached HEAD).
func TestCheckoutDetached(t *testing.T) {
	r := branchRepo(t)
	wip := strings.TrimSpace(r.git(t, "rev-parse", "wip"))
	if err := CheckoutDetached(r.dir, wip); err != nil {
		t.Fatal(err)
	}
	if cur := strings.TrimSpace(r.git(t, "branch", "--show-current")); cur != "" {
		t.Errorf("still on %q", cur)
	}
}

// GITX-2: a remote branch is deleted on the remote; the tracking ref
// goes too.
func TestDeleteRemoteBranch(t *testing.T) {
	r := branchRepo(t)
	bare := t.TempDir()
	c := exec.Command("git", "init", "-q", "--bare", bare)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r.git(t, "remote", "add", "origin", bare)
	r.git(t, "push", "-q", "origin", "main", "wip")
	r.git(t, "fetch", "-q", "origin")
	if err := DeleteRemoteBranch(r.dir, "origin/wip"); err != nil {
		t.Fatal(err)
	}
	if out := r.git(t, "branch", "-r"); strings.Contains(out, "origin/wip") {
		t.Errorf("tracking ref left: %q", out)
	}
	if _, err := os.Stat(bare + "/refs/heads/wip"); err == nil {
		t.Error("remote still has wip")
	}
	if err := DeleteRemoteBranch(r.dir, "nowhere"); err == nil {
		t.Error("accepted a name with no remote")
	}
}
