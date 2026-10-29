package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SCN-10: two windows on one folder. While one window's git holds the
// index lock, the other's stage and commit fail with a clear reason, not
// "exit status 128" or git's long "Unable to create '…/index.lock'".
func TestIndexLockGivesAClearReason(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	if err := Stage(r.dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Commit(r.dir, "first"); err != nil {
		t.Fatal(err)
	}
	r.write(t, "a.txt", "y\n")
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	const want = "Another git command is using this repository"
	for name, err := range map[string]error{
		"stage":   Stage(r.dir, "."),
		"unstage": Unstage(r.dir, "a.txt"),
		"commit":  CommitWith(r.dir, "second", false, false),
	} {
		if err == nil {
			t.Errorf("%s: no error with index.lock held", name)
			continue
		}
		if !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: %q, want it to start with %q", name, err.Error(), want)
		}
	}
	// The lock goes: everything works again.
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := Stage(r.dir, "."); err != nil {
		t.Fatal(err)
	}
	if err := Commit(r.dir, "second"); err != nil {
		t.Fatal(err)
	}
}
