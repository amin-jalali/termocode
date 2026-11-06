package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bulkRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "b.txt", "two\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "first")
	return r
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

// GITX-20: Unstage All takes every staged change out of the index and
// keeps it in the working tree.
func TestUnstageAll(t *testing.T) {
	r := bulkRepo(t)
	r.write(t, "a.txt", "ONE\n")
	r.write(t, "new.txt", "new\n")
	r.git(t, "add", ".")
	if err := UnstageAll(r.dir); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "diff", "--cached", "--name-only")); got != "" {
		t.Errorf("still staged: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "a.txt")); string(b) != "ONE\n" || !exists(r.dir, "new.txt") {
		t.Error("working tree changed")
	}
}

// GITX-20: on a repo with no commit yet, Unstage All still works (git
// reset has no HEAD to go back to there), also with nothing staged.
func TestUnstageAllNoCommitYet(t *testing.T) {
	r := newGitTestRepo(t)
	if err := UnstageAll(r.dir); err != nil {
		t.Fatalf("nothing staged: %v", err)
	}
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", ".")
	if err := UnstageAll(r.dir); err != nil {
		t.Fatal(err)
	}
	if got := r.git(t, "status", "--porcelain"); strings.TrimSpace(got) != "?? a.txt" {
		t.Errorf("status %q", got)
	}
}

// GITX-20: Discard All puts every tracked change (staged too) in a
// safety stash and leaves new files alone; the stash brings it all back.
func TestDiscardAllKeepsNewFiles(t *testing.T) {
	r := bulkRepo(t)
	r.write(t, "a.txt", "ONE\n")
	r.write(t, "b.txt", "TWO\n")
	r.git(t, "add", "b.txt")
	r.write(t, "new.txt", "new\n")
	ref, err := DiscardAll(r.dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "stash@{0}" {
		t.Errorf("ref %q", ref)
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "?? new.txt" {
		t.Errorf("status after discard %q", got)
	}
	if msg := r.git(t, "stash", "list"); !strings.Contains(msg, DiscardStashMessage) {
		t.Errorf("stash list %q", msg)
	}
	r.git(t, "stash", "pop", "-q")
	if b, _ := os.ReadFile(filepath.Join(r.dir, "b.txt")); string(b) != "TWO\n" {
		t.Error("the safety copy did not bring b.txt back")
	}
}

// GITX-20: with untracked, the new files go into the safety stash too.
func TestDiscardAllWithNewFiles(t *testing.T) {
	r := bulkRepo(t)
	r.write(t, "a.txt", "ONE\n")
	r.write(t, "dir/new.txt", "new\n")
	if _, err := DiscardAll(r.dir, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "" || exists(r.dir, "dir/new.txt") {
		t.Errorf("status after discard %q", got)
	}
	r.git(t, "stash", "pop", "-q")
	if !exists(r.dir, "dir/new.txt") {
		t.Error("new file not in the safety copy")
	}
}

// GITX-20: nothing to discard is ErrNothingToDiscard and makes no stash.
func TestDiscardAllNothing(t *testing.T) {
	r := bulkRepo(t)
	r.write(t, "new.txt", "new\n")
	if _, err := DiscardAll(r.dir, false); !errors.Is(err, ErrNothingToDiscard) {
		t.Errorf("err %v", err)
	}
	if got := r.git(t, "stash", "list"); got != "" {
		t.Errorf("stash made: %q", got)
	}
}

// GITX-20: Delete Untracked Files moves only the new files into a safety
// stash: tracked changes stay, ignored files stay.
func TestDeleteUntracked(t *testing.T) {
	r := bulkRepo(t)
	r.write(t, ".gitignore", "*.log\n")
	r.git(t, "add", ".gitignore")
	r.git(t, "commit", "-q", "-m", "ignore")
	r.write(t, "a.txt", "ONE\n")
	r.write(t, "new one.txt", "new\n")
	r.write(t, "dir/deep.txt", "deep\n")
	r.write(t, "keep.log", "log\n")
	ref, err := DeleteUntracked(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "stash@{0}" {
		t.Errorf("ref %q", ref)
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "M a.txt" {
		t.Errorf("status %q", got)
	}
	if exists(r.dir, "new one.txt") || exists(r.dir, "dir/deep.txt") || !exists(r.dir, "keep.log") {
		t.Error("wrong files removed")
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "a.txt")); string(b) != "ONE\n" {
		t.Error("tracked change lost")
	}
	r.git(t, "checkout", "--", "a.txt")
	r.git(t, "stash", "pop", "-q")
	if !exists(r.dir, "new one.txt") || !exists(r.dir, "dir/deep.txt") {
		t.Error("safety copy misses the new files")
	}
	if _, err := DeleteUntracked(r.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteUntracked(r.dir); !errors.Is(err, ErrNothingToDiscard) {
		t.Errorf("second run: %v", err)
	}
}
