package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WTX-14: archive removes a worktree's folder (the branch stays) and
// refuses a dirty one unless the caller says the changes go; a locked
// worktree and the main checkout are never removed; restore brings the
// folder back from the branch.
func TestRemoveAndRestoreWorktree(t *testing.T) {
	r := committedRepo(t)
	dir := filepath.Join(t.TempDir(), "app.worktrees", "feat-x")
	if err := AddWorktree(r.dir, dir, "feat/x", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := WorktreeDirty(dir); err != nil || n != 1 {
		t.Fatalf("dirty %d %v", n, err)
	}
	err := RemoveWorktree(r.dir, dir, false)
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("a dirty folder was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("the refused remove touched the folder")
	}

	// Locked: never removed, not even when the changes may go.
	r.git(t, "worktree", "lock", "--reason", "on a usb disk", dir)
	if err := RemoveWorktree(r.dir, dir, true); !errors.Is(err, ErrWorktreeLocked) || !strings.Contains(err.Error(), "on a usb disk") {
		t.Fatalf("locked: %v", err)
	}
	r.git(t, "worktree", "unlock", dir)

	if err := RemoveWorktree(r.dir, dir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the folder is still there")
	}
	if !BranchExists(r.dir, "feat/x") {
		t.Fatal("archive deleted the branch")
	}
	if err := RemoveWorktree(r.dir, r.dir, true); err == nil {
		t.Fatal("the main checkout was removed")
	}

	if err := RestoreWorktree(r.dir, dir, "feat/x"); err != nil {
		t.Fatal(err)
	}
	if br, _ := GetBranch(dir); br.Name != "feat/x" {
		t.Errorf("restored on %q", br.Name)
	}
	if err := RestoreWorktree(r.dir, dir, "feat/x"); err == nil {
		t.Error("restore over a folder that is there")
	}
	if err := RestoreWorktree(r.dir, filepath.Join(t.TempDir(), "y"), "no-such"); err == nil {
		t.Error("restore from a branch that is gone")
	}
}

// WTX-14: delete needs to know what the branch would lose: the commits
// not on the base branch. A merged branch deletes with -d; an unmerged
// one only when forced.
func TestBranchUnmergedAndDelete(t *testing.T) {
	r := committedRepo(t)
	if got := DefaultBranch(r.dir); got != "main" {
		t.Fatalf("default branch %q", got)
	}
	r.git(t, "branch", "merged")
	r.git(t, "checkout", "-q", "-b", "work")
	r.write(t, "a.txt", "work 1\n")
	r.git(t, "commit", "-qam", "w1")
	r.write(t, "a.txt", "work 2\n")
	r.git(t, "commit", "-qam", "w2")
	r.git(t, "checkout", "-q", "main")
	if n, err := BranchUnmerged(r.dir, "merged", "main"); err != nil || n != 0 {
		t.Fatalf("merged: %d %v", n, err)
	}
	if n, err := BranchUnmerged(r.dir, "work", "main"); err != nil || n != 2 {
		t.Fatalf("work: %d %v", n, err)
	}
	if err := DeleteBranch(r.dir, "work", false); err == nil {
		t.Fatal("an unmerged branch went without force")
	}
	if err := DeleteBranch(r.dir, "work", true); err != nil || BranchExists(r.dir, "work") {
		t.Fatalf("forced delete: %v", err)
	}
	if err := DeleteBranch(r.dir, "merged", false); err != nil || BranchExists(r.dir, "merged") {
		t.Fatalf("merged delete: %v", err)
	}
	if err := DeleteBranch(r.dir, "-D", true); err == nil {
		t.Error("a flag as a branch name")
	}
}

// WTX-15: prune drops git's records of worktrees whose folders are gone.
func TestPruneWorktrees(t *testing.T) {
	r := committedRepo(t)
	dir := filepath.Join(t.TempDir(), "gone")
	if err := AddWorktree(r.dir, dir, "gone", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := PruneWorktrees(r.dir); err != nil {
		t.Fatal(err)
	}
	list, _, err := ListWorktrees(r.dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("after prune: %+v %v", list, err)
	}
}
