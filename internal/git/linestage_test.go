package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lineRepo makes a repo with a.txt committed as l1..l6.
func lineRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "l1\nl2\nl3\nl4\nl5\nl6\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	return r
}

func (r *gitTestRepo) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// indexText is the file as the index holds it.
func (r *gitTestRepo) indexText(t *testing.T, name string) string {
	t.Helper()
	return r.git(t, "show", ":"+name)
}

func TestStageLinesOneLine(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nX2\nl3\nX4\nl5\nl6\n")
	if err := StageLines(r.dir, "a.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nl2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
	if got := r.read(t, "a.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("the working file must not change:\n%s", got)
	}
}

func TestStageLinesRange(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nA\nB\nC\nl5\nl6\nD\n")
	if err := StageLines(r.dir, "a.txt", 2, 3); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nA\nB\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
}

func TestUnstageLines(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nX2\nl3\nX4\nl5\nl6\n")
	r.git(t, "add", "a.txt")
	if err := UnstageLines(r.dir, "a.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nX2\nl3\nl4\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
	if got := r.read(t, "a.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("the working file must not change:\n%s", got)
	}
}

func TestDiscardLines(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nX2\nl3\nX4\nl5\nl6\n")
	if err := DiscardLines(r.dir, "a.txt", 2, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "a.txt"); got != "l1\nl2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("working file:\n%s", got)
	}
	if s := strings.TrimSpace(r.git(t, "diff", "--cached")); s != "" {
		t.Errorf("the index must not change:\n%s", s)
	}
}

func TestStageLinesOnlyAdditions(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nl2\nN1\nN2\nl3\nl4\nl5\nl6\n")
	if err := StageLines(r.dir, "a.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nl2\nN2\nl3\nl4\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
}

func TestStageLinesOnlyDeletions(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nl4\nl5\nl6\n")
	// The removed lines l2 and l3 sit before line 2 (l4). Choosing it takes both.
	if err := StageLines(r.dir, "a.txt", 2, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nl4\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
}

func TestStageLinesCRLF(t *testing.T) {
	r := newGitTestRepo(t)
	r.git(t, "config", "core.autocrlf", "false")
	r.write(t, "w.txt", "a\r\nb\r\nc\r\n")
	r.git(t, "add", "w.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "w.txt", "a\r\nB\r\nc\r\nD\r\n")
	if err := StageLines(r.dir, "w.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "w.txt"); got != "a\r\nb\r\nc\r\nD\r\n" {
		t.Errorf("index keeps CRLF: %q", got)
	}
	if err := DiscardLines(r.dir, "w.txt", 2, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.read(t, "w.txt"); got != "a\r\nb\r\nc\r\nD\r\n" {
		t.Errorf("worktree keeps CRLF: %q", got)
	}
}
