package diffview

import (
	"reflect"
	"strings"
	"testing"
)

// rowWith is the first card row that holds every one of parts.
func rowWith(s string, parts ...string) string {
next:
	for _, l := range strings.Split(s, "\n") {
		for _, p := range parts {
			if !strings.Contains(l, p) {
				continue next
			}
		}
		return l
	}
	return ""
}

// A rejected change says so on its row and in the footer; Undo is offered.
func TestRejectedChangeShows(t *testing.T) {
	m := demo(true)
	m, _ = key(m, "r") // change 1 rejected, focus on change 2
	s := plain(m)
	if !strings.Contains(s, "0 of 2 accepted · 1 rejected") {
		t.Errorf("footer does not count the rejected change:\n%s", s)
	}
	if !strings.Contains(rowWith(s, "Change 1 of 2 ·"), icons().cross) {
		t.Errorf("unfocused rejected change has no ✗ mark:\n%s", rowWith(s, "Change 1 of 2 ·"))
	}
	m, _ = key(m, "p")
	if l := rowWith(plain(m), "Change 1 of 2 ·"); !strings.Contains(l, "Undo") || !strings.Contains(l, "Rejected") {
		t.Errorf("focused rejected change row = %q, want Undo and Rejected", l)
	}
}

// The focused accepted change shows Accepted and Undo.
func TestAcceptedChangeShows(t *testing.T) {
	m := demo(true)
	m.SetDecision(0, Accepted)
	if l := rowWith(plain(m), "Change 1 of 2 ·"); !strings.Contains(l, "Accepted") || !strings.Contains(l, "Undo") {
		t.Errorf("focused accepted change row = %q", l)
	}
}

// After the app applies a file, its changes read "applied" / "not
// applied" and the file list marks it as partly applied.
func TestDoneFileShowsWhatWasApplied(t *testing.T) {
	files := []File{{Name: "x.txt", Old: oldText, New: newText}, {Name: "y.txt", Old: "1\n", New: "2\n"}}
	m := New("Review", files, true)
	m.SetSize(150, 40)
	m.SetDecision(0, Accepted)
	m.SetDecision(1, Rejected)
	m.MarkDone(0, "Applied 1 of 2 changes")
	m.Select(0)
	s := plain(m)
	for _, want := range []string{"Applied 1 of 2 changes", "applied", "not applied", "Next file"} {
		if !strings.Contains(s, want) {
			t.Errorf("done file misses %q:\n%s", want, s)
		}
	}
	if rowWith(s, "x.txt", icons().part) == "" {
		t.Errorf("file list does not mark x.txt as partly applied:\n%s", s)
	}
}

func TestDeletionHunkLabel(t *testing.T) {
	m := New("t", []File{{Name: "x", Old: "a\nb\nc\nd\n", New: "a\nd\n"}}, false)
	m.SetSize(120, 20)
	if s := plain(m); !strings.Contains(s, "removes line 2–3") {
		t.Errorf("no deletion label:\n%s", s)
	}
	m = New("t", []File{{Name: "x", Old: "a\nb\nc\n", New: "a\nc\n"}}, false)
	m.SetSize(120, 20)
	if s := plain(m); !strings.Contains(s, "removes line 2") || strings.Contains(s, "removes line 2–") {
		t.Errorf("one-line deletion label wrong:\n%s", s)
	}
}

func TestCutLeftHalvesWideRunes(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"abcdef", 2, "cdef"},
		{"x日本語", 2, " 本語"}, // 日 cut in half: a space keeps the columns
		{"x日本語", 3, "本語"},
		{"ab", 5, ""},
	}
	for _, c := range cases {
		if got := cutLeft(c.s, c.n); got != c.want {
			t.Errorf("cutLeft(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

func TestWrapWords(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  []string
	}{
		{"short", 10, []string{"short"}},
		{"aaa bbb ccc", 7, []string{"aaa bbb", "ccc"}},
		{"abcdefghij xy", 4, []string{"abcd", "efgh", "ij", "xy"}},
		{"go abcdefghij", 4, []string{"go", "abcd", "efgh", "ij"}},
		{"日本語テキスト", 4, []string{"日本", "語テ", "キス", "ト"}},
	}
	for _, c := range cases {
		if got := wrapWords(c.s, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("wrapWords(%q, %d) = %q, want %q", c.s, c.width, got, c.want)
		}
	}
}

// An open commit body wraps long lines and long words, skips blank lines
// and ends with a "more lines" row when it is too tall.
func TestCommitBodyWrapsAndFolds(t *testing.T) {
	h := commitHeader()
	h.Body = "First paragraph with enough words to wrap around the narrow card width easily.\n\n" +
		"https://example.com/" + strings.Repeat("x", 150) + "\nthird\nfourth\nfifth"
	m := NewCommit(h, commitFiles(), commitButtons())
	m.SetSize(80, 40) // a commit card opens with its body shown
	s := plain(m)
	if !strings.Contains(s, "First paragraph") || !strings.Contains(s, "more lines · r raw") {
		t.Errorf("open body:\n%s", s)
	}
	if strings.Contains(s, "fifth") {
		t.Errorf("body not cut to 3 rows:\n%s", s)
	}
}

func TestCommitAccessors(t *testing.T) {
	m := commitCard(150, 42)
	if h, ok := m.Header(); !ok || h.Hash != "a1b2c3d" {
		t.Errorf("Header() = %+v %v", h, ok)
	}
	if _, ok := demo(true).Header(); ok {
		t.Error("a review card has a commit header")
	}
	if f := m.File(2); f.Name != "logo.png" || !f.Binary {
		t.Errorf("File(2) = %+v", f)
	}
	if f := m.File(7); f.Name != "" {
		t.Errorf("File(7) = %+v, want zero", f)
	}
	m.SetFileText(7, "a", "b") // ignored
	m.SetFileNote(-1, "x")     // ignored
	if m.Files() != 3 {
		t.Errorf("files %d", m.Files())
	}
	hits := m.Hits()
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	for _, h := range hits {
		if h.ID == "" || h.W <= 0 {
			t.Errorf("bad hit %+v", h)
		}
		if got := m.ButtonAt(h.X, h.Y); got != h.ID {
			t.Errorf("ButtonAt(%d,%d) = %q, want %q", h.X, h.Y, got, h.ID)
		}
	}
}

// Deleted and renamed files are listed with their status letter.
func TestCommitListStatusLetters(t *testing.T) {
	files := []File{
		{Name: "gone.go", Status: 'D', Del: 3, Old: "a\nb\nc\n"},
		{Name: "pkg/new_name.go", Status: 'R', Tag: "renamed", Old: "x\n", New: "y\n"},
	}
	m := NewCommit(commitHeader(), files, commitButtons())
	m.SetSize(150, 42)
	s := plain(m)
	if !strings.Contains(lineWith(s, "gone.go"), " D ") || !strings.Contains(lineWith(s, "new_name.go"), " R ") {
		t.Errorf("status letters missing:\n%s", s)
	}
	m.Select(1)
	if !strings.Contains(plain(m), "renamed") {
		t.Errorf("tag not shown:\n%s", plain(m))
	}
}

func TestSyntaxHelpers(t *testing.T) {
	if !HasLexer("main.go") || HasLexer("notes.unknownext") {
		t.Error("HasLexer wrong")
	}
	m := New("t", []File{{Name: "a.go", Old: "package a\n", New: "package b\n"}}, false)
	if m.HasSyntax(0) || m.HasSyntax(5) {
		t.Error("HasSyntax before SetFileSyntax")
	}
	m.SetFileSyntax(0, Tokenize("a.go", "package a\n"), Tokenize("a.go", "package b\n"))
	if !m.HasSyntax(0) {
		t.Error("HasSyntax after SetFileSyntax")
	}
	if Tokenize("a.unknownext", "x") != nil || Tokenize("a.go", "") != nil {
		t.Error("Tokenize of an unknown type or empty text must be nil")
	}
}

// Control characters in a line are not drawn: a backspace or DEL would
// move the cursor and break the box.
func TestControlCharsNotDrawn(t *testing.T) {
	m := New("t", []File{{Name: "x", Old: "a\x01b\x08\x7fc\n", New: "a\x01b\x08\x7fc\nnew\n", Tag: "new file"}}, false)
	m.SetSize(120, 20)
	s := plain(m)
	if !strings.Contains(s, "abc") || strings.ContainsAny(s, "\x01\x08\x7f") {
		t.Errorf("control chars drawn:\n%q", s)
	}
	if !strings.Contains(s, "new file") {
		t.Errorf("file tag not shown:\n%s", s)
	}
}
