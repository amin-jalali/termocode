package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// moveOnDisk renames from to to in the repo like the explorer does and
// then lets the index follow.
func moveOnDisk(t *testing.T, r *gitTestRepo, from, to string) {
	t.Helper()
	a, b := filepath.Join(r.dir, from), filepath.Join(r.dir, to)
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndex(a, b); err != nil {
		t.Fatalf("MoveIndex: %v", err)
	}
}

func porcelain(t *testing.T, r *gitTestRepo) string {
	t.Helper()
	return strings.ReplaceAll(strings.TrimRight(r.git(t, "-c", "core.quotePath=false", "status", "--porcelain=v1"), "\n"), "\n", "|")
}

// A file with a staged change and unsaved-to-index edits: after the
// rename git sees main.go → app.go, the staged text moves with it and the
// working-tree edits stay unstaged.
func TestMoveIndexStagedFile(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "main.go", "package main\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "main.go", "package main\n// staged\n")
	r.git(t, "add", "main.go")
	r.write(t, "main.go", "package main\n// staged\n// unstaged\n")
	moveOnDisk(t, r, "main.go", "app.go")
	if got := porcelain(t, r); got != "RM main.go -> app.go" {
		t.Errorf("status %q", got)
	}
	if got := r.git(t, "show", ":app.go"); got != "package main\n// staged\n" {
		t.Errorf("index app.go %q", got)
	}
}

// A folder: every tracked file under it moves in the index; untracked
// files just move on disk.
func TestMoveIndexFolder(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "pkg/a.go", "package pkg\n\nfunc A() {}\n")
	r.write(t, "pkg/sub/b.go", "b\n")
	r.write(t, "pkgx.go", "x\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "pkg/a.go", "package pkg\n\nfunc A() {}\n\n// staged\n")
	r.git(t, "add", "pkg/a.go")
	r.write(t, "pkg/new.txt", "untracked\n")
	moveOnDisk(t, r, "pkg", "lib")
	want := "R  pkg/a.go -> lib/a.go|R  pkg/sub/b.go -> lib/sub/b.go|?? lib/new.txt"
	if got := porcelain(t, r); got != want {
		t.Errorf("status %q, want %q", got, want)
	}
}

// A name with glob characters is taken literally: renaming "a*.txt"
// leaves "ab.txt" alone.
func TestMoveIndexLiteralName(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "d/a*.txt", "star\n")
	r.write(t, "d/ab.txt", "ab\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	moveOnDisk(t, r, "d/a*.txt", "d/star.txt")
	if got := porcelain(t, r); got != "R  d/a*.txt -> d/star.txt" {
		t.Errorf("status %q", got)
	}
}

// Untracked files and folders outside git: nothing to do, no error.
func TestMoveIndexNothingTracked(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "keep.txt", "k\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "new.txt", "n\n")
	moveOnDisk(t, r, "new.txt", "other.txt")
	if got := porcelain(t, r); got != "?? other.txt" {
		t.Errorf("status %q", got)
	}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "x"), filepath.Join(dir, "y")
	if err := os.WriteFile(a, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if err := MoveIndex(a, b); err != nil {
		t.Errorf("outside a repo: %v", err)
	}
}

// A file in a merge conflict is left as it is (git mv refuses too).
func TestMoveIndexConflictLeftAlone(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "c.txt", "base\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "base")
	r.git(t, "checkout", "-q", "-b", "other")
	r.write(t, "c.txt", "other\n")
	r.git(t, "commit", "-q", "-am", "other")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "c.txt", "main\n")
	r.git(t, "commit", "-q", "-am", "main")
	merge := exec.Command("git", "merge", "-q", "other")
	merge.Dir = r.dir
	_ = merge.Run() // stops on the conflict
	moveOnDisk(t, r, "c.txt", "d.txt")
	if got := r.git(t, "ls-files", "-u"); !strings.Contains(got, "c.txt") {
		t.Errorf("conflict entries moved: %q", got)
	}
}
