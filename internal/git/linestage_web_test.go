package git

import (
	"errors"
	"strings"
	"testing"
)

// UnstageIndexLines takes lines from..to of the index's text (as a
// staged diff shows them) out of the index, even when unstaged edits
// moved the working file's rows; the working file keeps every change.
func TestUnstageIndexLines(t *testing.T) {
	r := lineRepo(t)
	r.write(t, "a.txt", "l1\nX2\nl3\nX4\nl5\nl6\n")
	r.git(t, "add", "a.txt")
	r.write(t, "a.txt", "new\nl1\nX2\nl3\nX4\nl5\nl6\n")
	if err := UnstageIndexLines(r.dir, "a.txt", 4, 4); err != nil {
		t.Fatal(err)
	}
	if got := r.indexText(t, "a.txt"); got != "l1\nX2\nl3\nl4\nl5\nl6\n" {
		t.Errorf("index:\n%s", got)
	}
	if got := r.read(t, "a.txt"); got != "new\nl1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("the working file must not change:\n%s", got)
	}
}

// DiscardHunkHeader reverts the one unstaged hunk with exactly that
// header in the working file; another hunk and the index stay; a header
// the diff no longer has changes nothing (ErrHunkGone).
func TestDiscardHunkHeader(t *testing.T) {
	r := newGitTestRepo(t)
	var lines []string
	for i := 1; i <= 14; i++ {
		lines = append(lines, "a"+string(rune('a'+i)))
	}
	orig := strings.Join(lines, "\n") + "\n"
	r.write(t, "a.txt", orig)
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	ch := append([]string(nil), lines...)
	ch[1], ch[12] = "TWO", "THIRTEEN"
	r.write(t, "a.txt", strings.Join(ch, "\n")+"\n")
	var heads []string
	for _, l := range strings.Split(r.git(t, "diff", "--no-color", "--", "a.txt"), "\n") {
		if strings.HasPrefix(l, "@@ ") {
			heads = append(heads, l)
		}
	}
	if len(heads) != 2 {
		t.Fatalf("want two hunks, got %q", heads)
	}
	if err := DiscardHunkHeader(r.dir, "a.txt", "@@ -1,1 +1,1 @@"); !errors.Is(err, ErrHunkGone) {
		t.Errorf("a header not in the diff: %v", err)
	}
	if err := DiscardHunkHeader(r.dir, "a.txt", heads[1]); err != nil {
		t.Fatal(err)
	}
	ch[12] = lines[12]
	if got := r.read(t, "a.txt"); got != strings.Join(ch, "\n")+"\n" {
		t.Errorf("working file:\n%s", got)
	}
	if s := strings.TrimSpace(r.git(t, "diff", "--cached")); s != "" {
		t.Errorf("the index must not change:\n%s", s)
	}
}
