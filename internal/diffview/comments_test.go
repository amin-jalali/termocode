package diffview

import (
	"strings"
	"testing"
)

// Spec (CMP-51): c on a change asks the app for a comment on the focused
// change, with the old text when there is one.
func TestCommentKeyAsksForTheFocusedChange(t *testing.T) {
	m := demo(false)
	m, _ = key(m, "n") // change 2
	m, msg := key(m, "c")
	ask, ok := msg.(CommentAskMsg)
	if !ok {
		t.Fatalf("c gave %T, want CommentAskMsg", msg)
	}
	if ask.File != 0 || ask.Hunk != 1 || ask.Text != "" || !strings.Contains(ask.Where, "x.txt") || !strings.Contains(ask.Where, "line 13") {
		t.Fatalf("ask = %+v", ask)
	}
	m.SetComment(0, 1, "use lower case")
	_, msg = key(m, "c")
	if ask := msg.(CommentAskMsg); ask.Text != "use lower case" {
		t.Fatalf("second ask has text %q, want the saved comment", ask.Text)
	}
}

// Spec: a comment shows under its change header; an empty text removes it.
func TestCommentShowsUnderItsChange(t *testing.T) {
	m := demo(true)
	m.SetComment(0, 0, "why upper case?")
	s := plain(m)
	if !strings.Contains(s, "why upper case?") {
		t.Fatalf("comment not shown:\n%s", s)
	}
	if !strings.Contains(s, "1 comment") {
		t.Fatalf("hint bar does not count the comments:\n%s", s)
	}
	if m.CommentCount() != 1 {
		t.Fatalf("count = %d", m.CommentCount())
	}
	m.SetComment(0, 0, "  ")
	if m.CommentCount() != 0 || strings.Contains(plain(m), "why upper case?") {
		t.Fatal("an empty comment must remove it")
	}
}

// Spec: S asks the app to send the comments; without comments it says so
// instead (nothing to send).
func TestSendKeyNeedsComments(t *testing.T) {
	m := demo(false)
	if _, msg := key(m, "S"); msg != nil {
		if _, ok := msg.(SendCommentsMsg); ok {
			t.Fatal("S without comments must not send")
		}
	}
	m.SetComment(0, 1, "x")
	if _, msg := key(m, "S"); msg == nil {
		t.Fatal("S sent nothing")
	} else if _, ok := msg.(SendCommentsMsg); !ok {
		t.Fatalf("S gave %T", msg)
	}
}

// Spec: Comments lists every comment in file / change order with the
// path, the lines and the changed code; FormatComments makes one prompt.
func TestCommentsBatchFormat(t *testing.T) {
	m := New("Changes of agent", []File{
		{Name: "a.go", Old: oldText, New: newText},
		{Name: "b.go", Old: "x\n", New: "y\n"},
	}, false)
	m.SetSize(120, 30)
	m.SetComment(1, 0, "rename y")
	m.SetComment(0, 1, "keep m lower case")
	m.SetComment(0, 0, "fine")
	cs := m.Comments()
	if len(cs) != 3 || cs[0].Path != "a.go" || cs[0].Hunk != 0 || cs[1].Hunk != 1 || cs[2].Path != "b.go" {
		t.Fatalf("order: %+v", cs)
	}
	if cs[1].Lines != "line 13" || !strings.Contains(cs[1].Code, "+M") || !strings.Contains(cs[1].Code, "-m") {
		t.Fatalf("comment 2 = %+v", cs[1])
	}
	out := FormatComments("Changes of agent", cs)
	for _, want := range []string{"3 review comments", "Changes of agent", "a.go (line 2)", "a.go (line 13)", "b.go (line 1)",
		"keep m lower case", "rename y", "```diff", "+M"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Index(out, "fine") > strings.Index(out, "keep m lower case") {
		t.Error("comments are not in change order")
	}
	m.ClearComments()
	if m.CommentCount() != 0 {
		t.Fatal("ClearComments left some")
	}
}

// Spec: the whitespace toggle diffs again; comments stay on their change.
func TestCommentsSurviveWhitespaceToggle(t *testing.T) {
	m := demo(false)
	m.SetComment(0, 1, "keep")
	m, _ = key(m, "w")
	if cs := m.Comments(); len(cs) != 1 || cs[0].Text != "keep" || cs[0].Lines != "line 13" {
		t.Fatalf("after w: %+v", cs)
	}
}
