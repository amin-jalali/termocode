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

func TestStageLinesNoNewlineAtEnd(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "n.txt", "a\nb\nc") // no newline at the end
	r.git(t, "add", "n.txt")
	r.git(t, "commit", "-q", "-m", "init")

	// A line added before the unterminated last line.
	r.write(t, "n.txt", "a\nNEW\nb\nc")
	if err := StageLines(r.dir, "n.txt", 2, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "n.txt"); got != "a\nNEW\nb\nc" {
		t.Errorf("index: %q", got)
	}

	// The last line changes and gets its newline: choose only the new text.
	r.write(t, "n.txt", "a\nNEW\nb\nC\n")
	if err := StageLines(r.dir, "n.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "n.txt"); got != "a\nNEW\nb\nC\n" {
		t.Errorf("index: %q", got)
	}
}

func TestStageLinesAppendAfterNoNewline(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "n.txt", "a\nb") // no newline at the end
	r.git(t, "add", "n.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "n.txt", "a\nb\nc\nd\n")
	// Staging only "d" cannot keep "b" without its newline and add "c" later.
	// Staging "c" and "d" together works.
	if err := StageLines(r.dir, "n.txt", 4, 4); err != ErrLineNewline {
		t.Fatalf("staging only the last of two appended lines: err = %v", err)
	}
	if err := StageLines(r.dir, "n.txt", 2, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "n.txt"); got != "a\nb\nc\nd\n" {
		t.Errorf("index: %q", got)
	}
}

func TestStageLinesNewFile(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "new.txt", "n1\nn2\nn3\n")
	if err := StageLines(r.dir, "new.txt", 2, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "new.txt"); got != "n2\n" {
		t.Errorf("index: %q", got)
	}
	if got := r.read(t, "new.txt"); got != "n1\nn2\nn3\n" {
		t.Errorf("the file must not change: %q", got)
	}
	// The next line goes on top of the first.
	if err := StageLines(r.dir, "new.txt", 3, 3); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "new.txt"); got != "n2\nn3\n" {
		t.Errorf("index: %q", got)
	}
}

func TestDiscardLinesUntrackedFile(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "new.txt", "n1\nn2\nn3\n")
	if err := DiscardLines(r.dir, "new.txt", 2, 2); err != ErrLineUntracked {
		t.Errorf("err = %v, want ErrLineUntracked", err)
	}
	if got := r.read(t, "new.txt"); got != "n1\nn2\nn3\n" {
		t.Errorf("the file must not change: %q", got)
	}
}

func TestLinesBinaryFile(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "b.bin", "a\x00b\x00c")
	r.git(t, "add", "b.bin")
	r.git(t, "commit", "-q", "-m", "bin")
	r.write(t, "b.bin", "a\x00B\x00c\x00d")
	if err := StageLines(r.dir, "b.bin", 1, 1); err != ErrLineBinary {
		t.Errorf("err = %v, want ErrLineBinary", err)
	}
}

func TestLinesRename(t *testing.T) {
	r := lineRepo(t)
	r.git(t, "mv", "a.txt", "b.txt")
	// Unstage looks at the index: it holds a rename of a.txt to b.txt.
	err := UnstageLines(r.dir, "b.txt", 2, 2)
	if err != ErrLineRename {
		t.Errorf("err = %v, want ErrLineRename", err)
	}
}

func TestUnstageLinesFileWithUnstagedEditsToo(t *testing.T) {
	r := lineRepo(t)
	// Staged: line 4 changed. Then two lines are added at the top, unstaged,
	// so the changed line is line 6 in the editor but line 4 in the index.
	r.write(t, "a.txt", "l1\nl2\nl3\nX4\nl5\nl6\n")
	r.git(t, "add", "a.txt")
	r.write(t, "a.txt", "top1\ntop2\nl1\nl2\nl3\nX4\nl5\nl6\n")
	if err := UnstageLines(r.dir, "a.txt", 6, 6); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nl2\nl3\nl4\nl5\nl6\n" {
		t.Errorf("index: %q", got)
	}
	// An unstaged line has nothing in the index to take out.
	if err := UnstageLines(r.dir, "a.txt", 1, 2); err != ErrNoLineChange {
		t.Errorf("err = %v, want ErrNoLineChange", err)
	}
}

func TestWorkToIndexLine(t *testing.T) {
	d := "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,3 +1,4 @@\n+top\n l1\n-l2\n+L2\n l3\n"
	m := workToIndex(d)
	for work, want := range map[int]int{1: -1, 2: 1, 3: -1, 4: 3, 5: 4, 9: 8} {
		if got := m(work); got != want {
			t.Errorf("work line %d -> %d, want %d", work, got, want)
		}
	}
}
