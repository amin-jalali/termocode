package git

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The background poll reads the status every 2 s. A plain `git status`
// refreshes and rewrites the index, taking .git/index.lock for that
// moment, so a commit or rebase --abort the user starts at the same time
// fails with "Another git command is using this repository". The status
// reads take no optional lock: the index stays as it is.
func TestStatusReadsLeaveTheIndexAlone(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	if err := Stage(r.dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Commit(r.dir, "first"); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(r.dir, ".git", "index")
	for name, read := range map[string]func() error{
		"Status":       func() error { _, err := Status(r.dir); return err },
		"StatusBranch": func() error { _, err := StatusBranch(r.dir); return err },
	} {
		// Same content, new time: the index entry is stale, so a status
		// with optional locks would refresh and rewrite it.
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(r.dir, "a.txt"), future, future); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(index)
		if err != nil {
			t.Fatal(err)
		}
		if err := read(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		after, _ := os.ReadFile(index)
		if !bytes.Equal(before, after) {
			t.Errorf("%s rewrote the index (it took the index lock)", name)
		}
	}
}
