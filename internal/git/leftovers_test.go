package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GITX-39 (B10, B19): an empty commit (--allow-empty) and a commit made
// as another author (--author); a bad author is refused before git runs.
func TestCommitEmptyAndAuthor(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")

	if err := CommitOpts(r.dir, "nothing", CommitOptions{}); err == nil {
		t.Fatal("a commit with nothing staged passed")
	}
	if err := CommitOpts(r.dir, "ci: run again", CommitOptions{AllowEmpty: true}); err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); got != "ci: run again" {
		t.Fatalf("subject %q", got)
	}

	for _, bad := range []string{"", "Ann", "Ann <>", "<ann@example.com>", "Ann ann@example.com", "--exec=x <a@b>", "Ann <a@b> x"} {
		if _, err := ParseAuthor(bad); err == nil {
			t.Errorf("ParseAuthor(%q) passed", bad)
		}
	}
	a, err := ParseAuthor("  Ann Lee   <ann@example.com> ")
	if err != nil || a != "Ann Lee <ann@example.com>" {
		t.Fatalf("ParseAuthor = %q, %v", a, err)
	}
	r.write(t, "a.txt", "two\n")
	r.git(t, "add", "a.txt")
	if err := CommitOpts(r.dir, "by Ann", CommitOptions{Author: a}); err != nil {
		t.Fatalf("author commit: %v", err)
	}
	if got := strings.TrimSpace(r.git(t, "log", "-1", "--format=%an <%ae>|%cn")); got != "Ann Lee <ann@example.com>|Test" {
		t.Fatalf("author|committer %q", got)
	}
}

// GITX-39 (B8): CommitPaths commits only the named files (new ones too),
// as they are on disk; what else is staged stays staged.
func TestCommitPaths(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.write(t, "gone.txt", "x\n")
	r.git(t, "add", "a.txt", "gone.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "two\n")
	r.write(t, "b.txt", "new\n")
	r.write(t, "c.txt", "staged\n")
	r.git(t, "add", "c.txt")
	if err := os.Remove(filepath.Join(r.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := CommitPaths(r.dir, "a, b and gone", []string{"a.txt", "b.txt", "gone.txt"}, CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(r.git(t, "show", "--name-status", "--format=%s", "HEAD")); got != "a, b and gone\n\nM\ta.txt\nA\tb.txt\nD\tgone.txt" {
		t.Fatalf("commit:\n%s", got)
	}
	if got := strings.TrimSpace(r.git(t, "status", "--porcelain")); got != "A  c.txt" {
		t.Fatalf("status %q", got)
	}
	if err := CommitPaths(r.dir, "none", nil, CommitOptions{}); err == nil {
		t.Error("a commit of no files passed")
	}
}
