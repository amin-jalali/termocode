package git

import (
	"strings"
	"testing"
)

// sideConflictRepo has main and side change the same line of f.txt.
func sideConflictRepo(t *testing.T) (*gitTestRepo, string) {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "f.txt", "base\n")
	r.git(t, "add", "f.txt")
	r.git(t, "commit", "-qm", "base")
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "f.txt", "side\n")
	r.git(t, "commit", "-qam", "side change")
	side := headOf(t, r)
	r.git(t, "checkout", "-q", "main")
	r.write(t, "f.txt", "main\n")
	r.git(t, "commit", "-qam", "main change")
	return r, side
}

func TestCherryPickRefusalsAndConflict(t *testing.T) {
	r, side := sideConflictRepo(t)
	if _, err := CherryPickMany(r.dir, nil); err == nil {
		t.Error("no commits: want error")
	}
	if _, err := CherryPickMany(r.dir, []string{"--abort"}); err == nil {
		t.Error("an option as commit must be refused")
	}
	if _, err := CherryPickMany(r.dir, []string{"deadbeefdeadbeef"}); err == nil {
		t.Error("an unknown commit: want git's error")
	}
	paths, err := CherryPickMany(r.dir, []string{side})
	if err == nil || !strings.Contains(err.Error(), "conflict") || len(paths) != 1 || paths[0] != "f.txt" {
		t.Errorf("conflict = %v, %v", paths, err)
	}
}

func TestRevertMergeParents(t *testing.T) {
	r, _ := sideConflictRepo(t)
	r.git(t, "merge", "-q", "-s", "ours", "-m", "merge side", "side")
	m := headOf(t, r)
	ps, err := MergeParents(r.dir, m)
	if err != nil || len(ps) != 2 {
		t.Fatalf("MergeParents = %v, %v", ps, err)
	}
	if _, err := MergeParents(r.dir, ps[0].Hash); err == nil || !strings.Contains(err.Error(), "not a merge") {
		t.Errorf("a plain commit = %v", err)
	}
	if _, err := MergeParents(r.dir, "nope"); err == nil {
		t.Error("an unknown commit: want error")
	}
	if _, err := RevertMerge(r.dir, m, 3); err == nil || !strings.Contains(err.Error(), "no parent 3") {
		t.Errorf("RevertMerge parent 3 = %v", err)
	}
	if _, err := RevertMerge(r.dir, "nope", 1); err == nil {
		t.Error("RevertMerge of an unknown commit: want error")
	}
}

func TestCommitListsDefaultLimit(t *testing.T) {
	r, side := sideConflictRepo(t)
	only, err := BranchOnlyCommits(r.dir, "side", 0)
	if err != nil || len(only) != 1 || !strings.HasPrefix(side, only[0].Hash) {
		t.Errorf("BranchOnlyCommits = %v, %v", only, err)
	}
	not, err := CommitsNotOn(r.dir, "side", 0)
	if err != nil || len(not) != 1 || not[0].Subject != "main change" {
		t.Errorf("CommitsNotOn = %v, %v", not, err)
	}
	if _, err := BranchOnlyCommits(r.dir, "nope", 5); err == nil {
		t.Error("an unknown branch: want error")
	}
	if _, err := CommitsNotOn(r.dir, "nope", 5); err == nil {
		t.Error("an unknown base: want error")
	}
}

func TestFileFromCommitGuards(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	if err := ApplyFileFromCommit(r.dir, "--x", "f1.txt"); err == nil {
		t.Error("an option as commit must be refused")
	}
	if _, err := RestoreFileFrom(r.dir, "--x", "f1.txt"); err == nil {
		t.Error("an option as commit must be refused")
	}
	if err := ApplyFileFromCommit(r.dir, "nope", "f1.txt"); err == nil {
		t.Error("an unknown commit: want error")
	}
	if _, err := RestoreFileFrom(r.dir, h[0], "f2.txt"); err == nil {
		t.Error("a file that is not in that commit: want error")
	}
	r.write(t, "f1.txt", "mine\n")
	backup, err := RestoreFileFrom(r.dir, h[0], "f1.txt")
	if err != nil || backup == "" {
		t.Fatalf("RestoreFileFrom = %q, %v", backup, err)
	}
	if got := r.git(t, "show", "HEAD:f1.txt"); got != "file 1\n" {
		t.Errorf("HEAD changed: %q", got)
	}
}

func TestRebaseOntoAndRowCommits(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	opt := RebaseOptions{}
	if _, err := RebaseOnto(r.dir, "--x", false, opt); err == nil {
		t.Error("an option as onto must be refused")
	}
	if _, err := RebaseOnto(r.dir, "nope", false, opt); err == nil || !strings.Contains(err.Error(), "no commit") {
		t.Errorf("unknown onto = %v", err)
	}
	if _, err := ResolveCommitName(r.dir, "--x"); err == nil {
		t.Error("an option as ref must be refused")
	}
	r.write(t, "f1.txt", "dirty\n")
	if _, err := RebaseOnto(r.dir, h[0], false, opt); err != ErrRebaseDirty {
		t.Errorf("dirty tree = %v", err)
	}
	r.git(t, "checkout", "-q", "f1.txt")
	// A commit that is not on this branch cannot be reworded or dropped.
	r.git(t, "checkout", "-q", "-b", "other", h[0])
	r.write(t, "o.txt", "o\n")
	r.git(t, "add", "o.txt")
	r.git(t, "commit", "-qm", "other work")
	off := headOf(t, r)
	r.git(t, "checkout", "-q", "main")
	if _, err := DropCommit(r.dir, off, opt); err == nil || !strings.Contains(err.Error(), "not on the current branch") {
		t.Errorf("DropCommit off branch = %v", err)
	}
	if _, err := RewordCommit(r.dir, h[1], "  ", opt); err == nil {
		t.Error("an empty message: want error")
	}
	if _, err := RewordCommit(r.dir, off, "new", opt); err == nil {
		t.Error("reword off branch: want error")
	}
	if _, err := SquashIntoPrevious(r.dir, h[0], opt); err == nil || !strings.Contains(err.Error(), "first commit") {
		t.Errorf("squash the first = %v", err)
	}
	out, err := RebaseOnto(r.dir, h[1], true, RebaseOptions{Autostash: true})
	if err != nil {
		t.Fatalf("RebaseOnto = %+v, %v", out, err)
	}
	if _, err := CommitPatch(r.dir, "nope"); err == nil {
		t.Error("CommitPatch of an unknown commit: want error")
	}
}
