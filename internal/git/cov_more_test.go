package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The container-aware stash type keeps the package functions when there
// is no Runner: the same actions, on this machine.
func TestStashRepoLocalForms(t *testing.T) {
	r := committedRepo(t)
	s := StashAt(r.dir, nil)
	if s.Hash("stash@{0}") != "" {
		t.Error("no stash yet")
	}
	r.write(t, "a.txt", "changed\n")
	if err := s.Push(StashOptions{Message: "one"}); err != nil {
		t.Fatal(err)
	}
	if l, err := s.List(); err != nil || len(l) != 1 || s.Hash("stash@{0}") == "" {
		t.Fatalf("List = %v, %v", l, err)
	}
	if d, err := s.Diff("stash@{0}"); err != nil || !strings.Contains(d, "+changed") {
		t.Errorf("Diff = %q, %v", d, err)
	}
	if err := s.Apply("stash@{0}"); err != nil {
		t.Fatal(err)
	}
	r.git(t, "checkout", "-q", "a.txt")
	if err := s.Drop("stash@{0}"); err != nil {
		t.Fatal(err)
	}
	r.write(t, "a.txt", "again\n")
	if err := s.Push(StashOptions{Message: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Pop(""); err != nil {
		t.Fatal(err)
	}
	r.git(t, "checkout", "-q", "a.txt")
	r.write(t, "a.txt", "third\n")
	if err := s.Push(StashOptions{Message: "three"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Pop("stash@{0}"); err != nil {
		t.Fatal(err)
	}
	r.git(t, "checkout", "-q", "a.txt")
	r.write(t, "a.txt", "fourth\n")
	if err := s.Push(StashOptions{Message: "four"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Branch("from-stash", "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "branch", "--show-current")); got != "from-stash" {
		t.Errorf("branch = %q", got)
	}
}

func TestOptionsAsRefsAreRefused(t *testing.T) {
	r := committedRepo(t)
	if _, err := BisectStart(r.dir, "HEAD", "--x"); err == nil {
		t.Error("BisectStart: an option as good must be refused")
	}
	if _, err := BisectRun(r.dir, r.dir, "  "); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Errorf("BisectRun without a command = %v", err)
	}
	if _, err := MergeBase(r.dir, "HEAD", "--x"); err == nil {
		t.Error("MergeBase: an option must be refused")
	}
	if _, err := WorkTreeDiffFiles(r.dir, "--x"); err == nil {
		t.Error("WorkTreeDiffFiles: an option must be refused")
	}
	if err := OpAbort(r.dir, "bogus"); err == nil || !strings.Contains(err.Error(), "cannot abort") {
		t.Errorf("OpAbort(bogus) = %v", err)
	}
	if err := OpContinue(r.dir, "bogus"); err == nil {
		t.Error("OpContinue(bogus): want error")
	}
}

func TestMoveIndexAllSameFolderOnly(t *testing.T) {
	r := committedRepo(t)
	err := MoveIndexAll([]Move{{From: filepath.Join(r.dir, "a.txt"), To: filepath.Join(r.dir, "sub", "a.txt")}})
	if err == nil || !strings.Contains(err.Error(), "same folder") {
		t.Errorf("MoveIndexAll across folders = %v", err)
	}
}

func TestRevertAndCherryPickOfAnUnknownCommit(t *testing.T) {
	r := committedRepo(t)
	if _, err := CherryPick(r.dir, "nope"); err == nil {
		t.Error("an unknown commit: want error")
	}
	if _, err := Revert(r.dir, "nope"); err == nil {
		t.Error("an unknown commit: want error")
	}
}

func TestPullRebaseSummaryAndFindReposCap(t *testing.T) {
	e := ReflogEntry{Kind: ReflogPull, Message: "pull --rebase (finish): returning to refs/heads/main"}
	if got := e.Summary(); got != "pull (rebase)" {
		t.Errorf("Summary = %q", got)
	}
	root := t.TempDir()
	for i := 0; i < findReposMax+5; i++ {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("r%03d", i), ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := FindRepos(root, 2); len(got) != findReposMax {
		t.Errorf("FindRepos found %d, want the cap %d", len(got), findReposMax)
	}
	if got := FindRepos(filepath.Join(root, "missing"), 2); len(got) != 0 {
		t.Errorf("a missing root finds %v", got)
	}
}
