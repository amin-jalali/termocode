package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A plan run's worktree starts from the last commit only: the user's
// uncommitted edits stay out of a branch that may be pushed. Its work is
// committed with a real message, so the branch can be shipped.
func TestAgentWorktreeFromHeadAndCommit(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "a.txt", "one\nMINE\nthree\n") // the user's uncommitted edit
	statusBefore := r.git(t, "status", "--porcelain")
	w, err := CreateAgentWorktreeFromHead(r.dir, "r1-plan")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(w.Path, "a.txt")); got != "one\ntwo\nthree\n" {
		t.Fatalf("worktree has the user's edit: %q", got)
	}
	if ok, err := CommitAgentWorktree(r.dir, "r1-plan", "feat: nothing"); ok || err != nil {
		t.Fatalf("commit with no changes = %v, %v", ok, err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := CommitAgentWorktree(r.dir, "r1-plan", "feat: add new.txt")
	if !ok || err != nil {
		t.Fatalf("commit = %v, %v", ok, err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s", w.Branch)); got != "feat: add new.txt" {
		t.Errorf("branch tip %q", got)
	}
	if after := r.git(t, "status", "--porcelain"); after != statusBefore {
		t.Errorf("user's status changed:\n%s", after)
	}
	if b := strings.TrimSpace(r.git(t, "branch", "--show-current")); b != "main" {
		t.Errorf("user's branch changed to %q", b)
	}
}
