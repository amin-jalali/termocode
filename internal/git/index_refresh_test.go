package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// FIX-40: status runs with --no-optional-locks, so it never writes the
// stat data it checked back to the index; a touched file stays
// "stat-dirty" and every status reads it again. RefreshIndex writes it
// back (git diff-files, which never refreshes, is the witness).
func TestRefreshIndexSettlesStatDirtyFiles(t *testing.T) {
	r := committedRepo(t)
	later := time.Now().Add(time.Hour)
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.Chtimes(filepath.Join(r.dir, f), later, later); err != nil {
			t.Fatal(err)
		}
	}
	if out := r.git(t, "diff-files", "--name-only"); !strings.Contains(out, "a.txt") {
		t.Fatalf("setup: the touched files are not stat-dirty: %q", out)
	}
	if _, err := StatusBranch(r.dir); err != nil {
		t.Fatal(err)
	}
	if out := r.git(t, "diff-files", "--name-only"); !strings.Contains(out, "a.txt") {
		t.Fatalf("status wrote the index (it must not take the lock): %q", out)
	}
	if err := RefreshIndex(r.dir); err != nil {
		t.Fatal(err)
	}
	if out := r.git(t, "diff-files", "--name-only"); strings.TrimSpace(out) != "" {
		t.Errorf("after RefreshIndex still stat-dirty: %q", out)
	}
}

// A held index.lock (another git command) is left alone: no error toast,
// no wait, the lock file is not touched.
func TestRefreshIndexSkipsAHeldLock(t *testing.T) {
	r := committedRepo(t)
	lock := filepath.Join(r.dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RefreshIndex(r.dir); err != nil {
		t.Errorf("RefreshIndex with a held lock = %v, want a quiet skip", err)
	}
	if b, err := os.ReadFile(lock); err != nil || string(b) != "theirs" {
		t.Errorf("the other command's lock changed: %q %v", b, err)
	}
}
