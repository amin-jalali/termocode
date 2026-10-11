package specui

import (
	"regexp"
	"strings"
	"testing"
)

var countRe = regexp.MustCompile(`(\d+)/(\d+)`)

// findCount reads "cur/total" from the find panel row.
func findCount(t *testing.T, row string) (cur, total string) {
	t.Helper()
	m := countRe.FindStringSubmatch(row)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// Spec: Given Find is open on a query with two matches,
// when the user presses Enter twice,
// then the cursor jumps to the first and then the second match (n/2
// counter and the status bar line follow), and Shift+Tab goes back.
func TestFindInFileNextPrevMovesCursor(t *testing.T) {
	files := map[string]string{"a.txt": "alpha\nneedle one\nbeta\nneedle two\n"}
	d := editorWith(t, files, "a.txt", "needle two")
	d.Key("ctrl+f")
	d.Type("needle")
	d.Key("enter")
	if cur, n := findCount(t, lineWith(d, "Aa ab .*")); cur != "1" || n != "2" {
		t.Errorf("after Enter: %s/%s, want 1/2\n%s", cur, n, screen(d))
	}
	if !strings.Contains(screen(d), "Ln 2,") {
		t.Errorf("cursor not on line 2:\n%s", lineWith(d, "Ln "))
	}
	d.Key("enter")
	if cur, _ := findCount(t, lineWith(d, "Aa ab .*")); cur != "2" || !strings.Contains(screen(d), "Ln 4,") {
		t.Errorf("after 2nd Enter want 2/2 on Ln 4:\n%s", screen(d))
	}
	d.Key("shift+tab")
	if !strings.Contains(screen(d), "Ln 2,") {
		t.Errorf("Shift+Tab did not go back to line 2:\n%s", lineWith(d, "Ln "))
	}
	d.Key("esc")
	mustNotSee(t, d, "Aa ab .*")
}

// Spec: Given Replace with whole word and case on,
// when the user replaces "Cat" with "a/b&c" (chars special to :s),
// then only the whole, same-case word changes and the replacement is
// literal.
func TestReplaceInFileWholeWordCaseLiteral(t *testing.T) {
	files := map[string]string{"pets.txt": "Cat cat Cats Cat.\n"}
	d := editorWith(t, files, "pets.txt", "Cats")
	d.Key("ctrl+h")
	clickText(t, d, "Aa")
	clickText(t, d, "ab ")
	d.Type("Cat")
	d.Key("tab")
	d.Type("a/b&c")
	d.Key("alt+enter")
	d.Key("esc")
	d.Key("ctrl+s")
	want := "a/b&c cat Cats a/b&c.\n"
	if body := waitFile(t, d, "pets.txt", func(s string) bool { return s == want }); body != want {
		t.Fatalf("got %q, want %q", body, want)
	}
}
