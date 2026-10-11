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
		t.Errorf("cursor not on line 2:\n%s", statusBar(d))
	}
	d.Key("enter")
	if cur, _ := findCount(t, lineWith(d, "Aa ab .*")); cur != "2" || !strings.Contains(screen(d), "Ln 4,") {
		t.Errorf("after 2nd Enter want 2/2 on Ln 4:\n%s", screen(d))
	}
	d.Key("shift+tab")
	if !strings.Contains(screen(d), "Ln 2,") {
		t.Errorf("Shift+Tab did not go back to line 2:\n%s", statusBar(d))
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

// Spec: Given Find is open (Ctrl+F) with a query,
// when the user presses Ctrl+H, then Ctrl+F, then replaces all and
// presses Ctrl+S without closing the bar,
// then the Replace row opens and closes, and the file is saved.
func TestFindBarKeepsEditorKeys(t *testing.T) {
	files := map[string]string{"pets.txt": "cat cat\n"}
	d := editorWith(t, files, "pets.txt", "cat cat")
	d.Key("ctrl+f")
	d.Type("cat")
	d.Key("ctrl+h")
	if !strings.Contains(screen(d), "↵ ⇒") {
		t.Fatalf("Ctrl+H in the open find bar did not show the Replace row:\n%s", screen(d))
	}
	d.Key("ctrl+f")
	mustNotSee(t, d, "↵ ⇒")
	d.Key("ctrl+h") // focus moves to Replace: Find has a query
	d.Type("dog")
	d.Key("alt+enter", "ctrl+s")
	if got := waitFile(t, d, "pets.txt", func(s string) bool { return s == "dog dog\n" }); got != "dog dog\n" {
		t.Fatalf("Ctrl+S in the find bar did not save: %q", got)
	}
}

// replaceSteps opens Replace, fills find / replace, presses Enter n
// times, closes the bar, saves and returns the file.
func replaceSteps(t *testing.T, files map[string]string, rel, shown, find, repl string, n int) string {
	t.Helper()
	d := editorWith(t, files, rel, shown)
	d.Key("ctrl+h")
	d.Type(find)
	d.Key("tab")
	d.Type(repl)
	for range n {
		d.Key("enter")
	}
	d.Key("esc", "ctrl+s")
	orig := files[rel]
	return waitFile(t, d, rel, func(s string) bool { return s != orig })
}

// Spec: Given the cursor on the first of three "cat" matches,
// when the user presses Enter in the Replace field once, twice, three
// times, then the matches change one by one from the current one on
// (first, then "concat", then "the cat").
func TestReplaceInFileStartsAtCurrentMatch(t *testing.T) {
	files := map[string]string{"pets.txt": "cat\nconcat\nthe cat sat\n"}
	want := []string{
		"dog\nconcat\nthe cat sat\n",
		"dog\ncondog\nthe cat sat\n",
		"dog\ncondog\nthe dog sat\n",
	}
	for i, w := range want {
		if got := replaceSteps(t, files, "pets.txt", "the cat sat", "cat", "dog", i+1); got != w {
			t.Errorf("after %d× Enter: got %q, want %q", i+1, got, w)
		}
	}
}

// Spec: Given a replacement that contains the query ("cat" → "cats"),
// when the user presses Enter once per match,
// then each match is replaced once — never "catss" in the same place.
func TestReplaceInFileReplacementContainsQuery(t *testing.T) {
	files := map[string]string{"pets.txt": "cat\nthe cat sat\n"}
	if got, want := replaceSteps(t, files, "pets.txt", "the cat sat", "cat", "cats", 2), "cats\nthe cats sat\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Spec: Given a file with "foo" in several cases and words,
// when the user opens Find (Ctrl+F) and types "foo",
// then the panel counts every case-insensitive match; the Aa, ab and .*
// toggles narrow the count to case-sensitive, whole-word and regex
// matches, and the regex takes VS Code (JavaScript) syntax: "fo+b".
func TestFindInFileCountsAndToggles(t *testing.T) {
	files := map[string]string{"a.txt": "foo Foo foobar\nfoo FOO xfoo\nfob fooob\n"}
	d := editorWith(t, files, "a.txt", "foobar")
	d.Key("ctrl+f")
	d.Type("foo")
	total := func() string {
		t.Helper()
		_, n := findCount(t, lineWith(d, "Aa ab .*"))
		return n
	}
	if got := total(); got != "7" {
		t.Fatalf("case-insensitive count = %q, want 7\n%s", got, screen(d))
	}
	clickText(t, d, "Aa") // case sensitive
	if got := total(); got != "5" {
		t.Errorf("case-sensitive count = %q, want 5\n%s", got, screen(d))
	}
	clickText(t, d, "ab ") // + whole word
	if got := total(); got != "2" {
		t.Errorf("case + whole word count = %q, want 2\n%s", got, screen(d))
	}
	clickText(t, d, "ab ") // whole word off
	clickText(t, d, ".*")  // regex on
	d.Key("backspace", "backspace", "backspace")
	d.Type("fo+b")
	if got := total(); got != "3" {
		t.Errorf("regex fo+b count = %q, want 3 (foobar, fob, fooob)\n%s", got, screen(d))
	}
	d.Key("backspace", "backspace", "backspace", "backspace")
	d.Type("(fob|xfoo)")
	if got := total(); got != "2" {
		t.Errorf("regex (fob|xfoo) count = %q, want 2\n%s", got, screen(d))
	}
}

// Spec: Given Replace in regex mode,
// when the user replaces `(\w+)@(\w+)` with `$2 at $1` (Alt+Enter: all),
// then each match is rewritten with its groups swapped.
func TestReplaceInFileRegexGroups(t *testing.T) {
	files := map[string]string{"mail.txt": "ann@home\nbob@work\n"}
	d := editorWith(t, files, "mail.txt", "bob@work")
	d.Key("ctrl+h")
	clickText(t, d, ".*")
	d.Type(`(\w+)@(\w+)`)
	d.Key("tab")
	d.Type("$2 at $1")
	d.Key("alt+enter", "ctrl+s")
	want := "home at ann\nwork at bob\n"
	if got := waitFile(t, d, "mail.txt", func(s string) bool { return s == want }); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
