package git

import (
	"os"
	"path/filepath"
	"testing"
)

// A staged file moved to another folder (as the AI's rename tool can do)
// keeps its staged text: git shows a rename, not a delete plus untracked.
func TestMoveIndexAcrossFolders(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "src/main.go", "package main\n")
	r.write(t, "keep.txt", "k\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "src/main.go", "package main\n// staged\n")
	r.git(t, "add", "src/main.go")
	a, b := filepath.Join(r.dir, "src", "main.go"), filepath.Join(r.dir, "cmd", "app", "main.go")
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndexAcross(a, b); err != nil {
		t.Fatalf("MoveIndexAcross: %v", err)
	}
	if got := porcelain(t, r); got != "R  src/main.go -> cmd/app/main.go" {
		t.Errorf("status %q", got)
	}
	if got := r.git(t, "show", ":cmd/app/main.go"); got != "package main\n// staged\n" {
		t.Errorf("index %q", got)
	}
}

// A move into another repo (or out of git) leaves the index alone.
func TestMoveIndexAcrossOtherRepo(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	out := t.TempDir()
	a, b := filepath.Join(r.dir, "a.txt"), filepath.Join(out, "a.txt")
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndexAcross(a, b); err != nil {
		t.Fatalf("MoveIndexAcross: %v", err)
	}
	if got := porcelain(t, r); got != " D a.txt" {
		t.Errorf("status %q", got)
	}
}
