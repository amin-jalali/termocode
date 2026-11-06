package git

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// AddWorktree (WTX-5) makes a user worktree on a new branch from a ref;
// the branch and folder rules are checked first, and a failed add leaves
// nothing behind.
func TestAddWorktree(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "branch", "base")
	r.write(t, "a.txt", "changed on main\n")
	r.git(t, "commit", "-qam", "main moves on")
	dir := filepath.Join(t.TempDir(), "app.worktrees", "feat-x")
	if err := AddWorktree(r.dir, dir, "feat/x", "base"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("the worktree does not start from base: %q %v", b, err)
	}
	if br, _ := GetBranch(dir); br.Name != "feat/x" {
		t.Errorf("branch %q", br.Name)
	}

	// The branch is there now: a second add with it fails on the card,
	// and its folder is never made.
	again := filepath.Join(t.TempDir(), "again")
	err = AddWorktree(r.dir, again, "feat/x", "main")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("branch exists: %v", err)
	}
	if _, err := os.Stat(again); !os.IsNotExist(err) {
		t.Error("a failed add left its folder")
	}
	// A folder that is there is never used.
	if err := AddWorktree(r.dir, dir, "feat/y", "main"); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("folder exists: %v", err)
	}
	// An unknown ref: git fails, and the folder it would have made is gone.
	bad := filepath.Join(t.TempDir(), "deep", "bad")
	if err := AddWorktree(r.dir, bad, "feat/z", "no-such-ref"); err == nil {
		t.Fatal("an unknown ref made a worktree")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Error("a failed add left its folder")
	}
	if _, err := os.Stat(filepath.Dir(bad)); !os.IsNotExist(err) {
		t.Error("a failed add left the parent folder it made")
	}
	if out := r.git(t, "branch", "--list", "feat/z"); strings.TrimSpace(out) != "" {
		t.Errorf("a failed add left its branch: %q", out)
	}
}

// CheckBranchName is git's own rule for a new branch name.
func TestCheckBranchName(t *testing.T) {
	r := committedRepo(t)
	for _, ok := range []string{"feat/pin-row", "fix-1", "a/b/c"} {
		if err := CheckBranchName(r.dir, ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "a..b", "a b", "x.lock", "a//b/", "@", "HEAD"} {
		if err := CheckBranchName(r.dir, bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

// WorktreeRefs lists what a worktree can start from: the current branch
// first, then local branches, remote branches and tags.
func TestWorktreeRefs(t *testing.T) {
	r := committedRepo(t)
	r.git(t, "branch", "zeta")
	r.git(t, "tag", "v1")
	r.git(t, "checkout", "-q", "-b", "work")
	refs, cur, err := WorktreeRefs(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if cur != "work" || len(refs) == 0 || refs[0] != "work" {
		t.Fatalf("current %q refs %v", cur, refs)
	}
	for _, want := range []string{"zeta", "v1"} {
		if !slices.Contains(refs, want) {
			t.Errorf("%q missing from %v", want, refs)
		}
	}
	if slices.Index(refs, "v1") < slices.Index(refs, "zeta") {
		t.Errorf("tags before branches: %v", refs)
	}
}

// RemoteHosts (WTX-9) lists the hosts of the repository's remotes, once
// each, in name order; a repository without remotes has none.
func TestRemoteHosts(t *testing.T) {
	r := committedRepo(t)
	if h := RemoteHosts(r.dir); len(h) != 0 {
		t.Errorf("no remotes: %v", h)
	}
	r.git(t, "remote", "add", "origin", "git@github.com:o/r.git")
	r.git(t, "remote", "add", "work", "https://user@git.example.com/team/app.git")
	r.git(t, "remote", "add", "zz", "https://github.com/o/fork.git")
	if h := RemoteHosts(r.dir); !slices.Equal(h, []string{"github.com", "git.example.com"}) {
		t.Errorf("hosts %v", h)
	}
}

// ForgetWorktree (W2 leftover) drops git's record of one worktree whose
// folder is gone, and never touches one whose folder is still there.
func TestForgetWorktree(t *testing.T) {
	r := committedRepo(t)
	base := t.TempDir()
	gone, kept := filepath.Join(base, "gone"), filepath.Join(base, "kept")
	r.git(t, "worktree", "add", "-q", "-b", "gone", gone)
	r.git(t, "worktree", "add", "-q", "-b", "kept", kept)
	if err := ForgetWorktree(r.dir, kept); err == nil {
		t.Fatal("a worktree with its folder was forgotten")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the folder was touched: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if err := ForgetWorktree(r.dir, gone); err != nil {
		t.Fatal(err)
	}
	list, _, err := ListWorktrees(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, w := range list {
		paths = append(paths, filepath.Base(w.Path))
	}
	if slices.Contains(paths, "gone") || !slices.Contains(paths, "kept") {
		t.Errorf("worktrees after forget: %v", paths)
	}
}
