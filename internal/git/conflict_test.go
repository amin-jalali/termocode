package git

import (
	"reflect"
	"testing"
)

const twoConflicts = `top
<<<<<<< HEAD
ours 1
ours 2
=======
theirs 1
>>>>>>> feature
middle
<<<<<<< HEAD
=======
only theirs
>>>>>>> feature
bottom
`

func TestParseConflictsBasic(t *testing.T) {
	bs := ParseConflicts(twoConflicts)
	if len(bs) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(bs))
	}
	b := bs[0]
	if b.Start != 1 || b.Sep != 4 || b.End != 6 || b.Base != -1 {
		t.Errorf("block 0 lines = %+v", b)
	}
	if b.OursLabel != "HEAD" || b.TheirsLabel != "feature" {
		t.Errorf("labels = %q / %q", b.OursLabel, b.TheirsLabel)
	}
	if !reflect.DeepEqual(b.Ours, []string{"ours 1", "ours 2"}) || !reflect.DeepEqual(b.Theirs, []string{"theirs 1"}) {
		t.Errorf("bodies = %q / %q", b.Ours, b.Theirs)
	}
	if len(bs[1].Ours) != 0 || !reflect.DeepEqual(bs[1].Theirs, []string{"only theirs"}) {
		t.Errorf("block 1 bodies = %q / %q", bs[1].Ours, bs[1].Theirs)
	}
}

func TestParseConflictsDiff3(t *testing.T) {
	text := "<<<<<<< ours\na\n||||||| base\no\n=======\nb\n>>>>>>> theirs\n"
	bs := ParseConflicts(text)
	if len(bs) != 1 {
		t.Fatalf("want 1, got %d", len(bs))
	}
	b := bs[0]
	if b.Base != 2 || b.BaseLabel != "base" {
		t.Errorf("base = %d %q", b.Base, b.BaseLabel)
	}
	if !reflect.DeepEqual(b.Ours, []string{"a"}) || !reflect.DeepEqual(b.BaseLn, []string{"o"}) || !reflect.DeepEqual(b.Theirs, []string{"b"}) {
		t.Errorf("bodies = %q %q %q", b.Ours, b.BaseLn, b.Theirs)
	}
	if got := ResolveConflict(text, b, AcceptBoth); got != "a\nb\n" {
		t.Errorf("both = %q", got)
	}
}

func TestParseConflictsMalformed(t *testing.T) {
	cases := map[string]string{
		"no separator": "<<<<<<< a\nx\n>>>>>>> b\n",
		"no end":       "<<<<<<< a\nx\n=======\ny\n",
		"long rule":    "<<<<<<< a\nx\n========\ny\n>>>>>>> b\n",
		"none":         "plain\ntext\n",
		"empty":        "",
	}
	for name, text := range cases {
		if bs := ParseConflicts(text); len(bs) != 0 {
			t.Errorf("%s: want 0 blocks, got %d", name, len(bs))
		}
	}
	// A dangling start before a valid block is skipped; the valid one parses.
	text := "<<<<<<< stray\n<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n"
	bs := ParseConflicts(text)
	if len(bs) != 1 || bs[0].Start != 1 {
		t.Fatalf("nested: got %+v", bs)
	}
}

func TestResolveConflict(t *testing.T) {
	bs := ParseConflicts(twoConflicts)
	ours := ResolveConflict(twoConflicts, bs[0], AcceptOurs)
	want := "top\nours 1\nours 2\nmiddle\n<<<<<<< HEAD\n=======\nonly theirs\n>>>>>>> feature\nbottom\n"
	if ours != want {
		t.Errorf("ours:\n%q\nwant\n%q", ours, want)
	}
	theirs := ResolveConflict(twoConflicts, bs[1], AcceptTheirs)
	if want := "top\n<<<<<<< HEAD\nours 1\nours 2\n=======\ntheirs 1\n>>>>>>> feature\nmiddle\nonly theirs\nbottom\n"; theirs != want {
		t.Errorf("theirs:\n%q", theirs)
	}
	// Ours side of block 1 is empty → resolution drops the region entirely.
	if got := ResolveConflict(twoConflicts, bs[1], AcceptOurs); got != "top\n<<<<<<< HEAD\nours 1\nours 2\n=======\ntheirs 1\n>>>>>>> feature\nmiddle\nbottom\n" {
		t.Errorf("empty ours: %q", got)
	}
}

func TestResolveConflictCRLFAndNoTrailingNewline(t *testing.T) {
	text := "a\r\n<<<<<<< HEAD\r\nx\r\n=======\r\ny\r\n>>>>>>> b\r\nz"
	bs := ParseConflicts(text)
	if len(bs) != 1 || bs[0].TheirsLabel != "b" || bs[0].Ours[0] != "x" {
		t.Fatalf("parse: %+v", bs)
	}
	if got := ResolveConflict(text, bs[0], AcceptBoth); got != "a\r\nx\r\ny\r\nz" {
		t.Errorf("got %q", got)
	}
}

func TestConflictAtAndHasConflicts(t *testing.T) {
	bs := ParseConflicts(twoConflicts)
	if _, ok := ConflictAt(bs, 0); ok {
		t.Error("line 0 is outside any block")
	}
	if b, ok := ConflictAt(bs, 6); !ok || b.Start != 1 {
		t.Error("line 6 (end marker) belongs to block 0")
	}
	if b, ok := ConflictAt(bs, 9); !ok || b.Start != 8 {
		t.Error("line 9 belongs to block 1")
	}
	if !HasConflicts(twoConflicts) || HasConflicts("a\nb") {
		t.Error("HasConflicts mismatch")
	}
}

func TestFileStatusConflicted(t *testing.T) {
	for _, c := range []string{"UU", "AA", "DD", "AU", "UA", "DU", "UD"} {
		if !(FileStatus{Code: c}).Conflicted() {
			t.Errorf("%s should be conflicted", c)
		}
	}
	for _, c := range []string{"M ", " M", "MM", "??", "A "} {
		if (FileStatus{Code: c}).Conflicted() {
			t.Errorf("%s should not be conflicted", c)
		}
	}
}
