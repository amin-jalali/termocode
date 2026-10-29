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

func TestReplacementTheirsFirstAndBase(t *testing.T) {
	text := "<<<<<<< HEAD\no\n||||||| base\nb\n=======\nt\n>>>>>>> x\n"
	b := ParseConflicts(text)[0]
	if got := b.Replacement(AcceptBothTheirsFirst); !reflect.DeepEqual(got, []string{"t", "o"}) {
		t.Errorf("theirs first = %q", got)
	}
	if got := b.Replacement(AcceptBase); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("base = %q", got)
	}
	if got := ResolveConflict(text, b, AcceptBase); got != "b\n" {
		t.Errorf("resolve base = %q", got)
	}
	// No diff3 section: the caller gives the base lines.
	b2 := ParseConflicts("<<<<<<< HEAD\no\n=======\nt\n>>>>>>> x")[0]
	if got := b2.Replacement(AcceptBase); len(got) != 0 {
		t.Errorf("unknown base = %q", got)
	}
	b2.BaseLn = []string{"from index"}
	if got := ResolveConflict("<<<<<<< HEAD\no\n=======\nt\n>>>>>>> x", b2, AcceptBase); got != "from index" {
		t.Errorf("given base, no final newline = %q", got)
	}
}

func TestResolveEveryWayCRLF(t *testing.T) {
	text := "a\r\n<<<<<<< HEAD\r\no\r\n||||||| base\r\nb\r\n=======\r\nt\r\n>>>>>>> x\r\nz\r\n"
	b := ParseConflicts(text)[0]
	want := map[Resolution]string{
		AcceptOurs:            "a\r\no\r\nz\r\n",
		AcceptTheirs:          "a\r\nt\r\nz\r\n",
		AcceptBoth:            "a\r\no\r\nt\r\nz\r\n",
		AcceptBothTheirsFirst: "a\r\nt\r\no\r\nz\r\n",
		AcceptBase:            "a\r\nb\r\nz\r\n",
	}
	for r, w := range want {
		if got := ResolveConflict(text, b, r); got != w {
			t.Errorf("resolution %d = %q, want %q", r, got, w)
		}
	}
}

func TestParseKeepsMarkerLookalikes(t *testing.T) {
	// 8-char runs and a Markdown underline inside the bodies are text.
	text := "<<<<<<< HEAD\n<<<<<<<< not a marker\nTitle\n========\n=======\n>>>>>>>> nope\nt\n>>>>>>> x\n"
	bs := ParseConflicts(text)
	if len(bs) != 1 {
		t.Fatalf("blocks = %d", len(bs))
	}
	if want := []string{"<<<<<<<< not a marker", "Title", "========"}; !reflect.DeepEqual(bs[0].Ours, want) {
		t.Errorf("ours = %q", bs[0].Ours)
	}
	if want := []string{">>>>>>>> nope", "t"}; !reflect.DeepEqual(bs[0].Theirs, want) {
		t.Errorf("theirs = %q", bs[0].Theirs)
	}
}

func TestResolveManyBlocksOneAtATime(t *testing.T) {
	var sb []byte
	for i := 0; i < 50; i++ {
		sb = append(sb, "ctx\n<<<<<<< HEAD\no\n=======\nt\n>>>>>>> x\n"...)
	}
	text := string(sb)
	for i := 0; i < 50; i++ {
		bs := ParseConflicts(text)
		if len(bs) != 50-i {
			t.Fatalf("step %d: %d blocks", i, len(bs))
		}
		r := AcceptOurs
		if i%2 == 1 {
			r = AcceptTheirs
		}
		text = ResolveConflict(text, bs[0], r)
	}
	if HasConflicts(text) {
		t.Fatal("markers left")
	}
	if got := len(SplitLines(text)); got != 100 {
		t.Errorf("lines = %d", got)
	}
}
