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
