package git

import (
	"strings"
	"testing"
)

const diffReplace = `diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,5 +1,6 @@
 one
-two
-three
+TWO
+THREE
+extra
 four
 five
`

func TestBuildLinePatchStage(t *testing.T) {
	cases := []struct {
		name     string
		from, to int
		want     string // the body after the hunk header
		hdr      string
	}{
		{"first added line takes the removed lines", 2, 2, " one\n-two\n-three\n+TWO\n four\n five\n", "@@ -1,5 +1,4 @@"},
		{"second added line only", 3, 3, " one\n two\n three\n+THREE\n four\n five\n", "@@ -1,5 +1,6 @@"},
		{"third added line only", 4, 4, " one\n two\n three\n+extra\n four\n five\n", "@@ -1,5 +1,6 @@"},
		{"whole change", 2, 4, " one\n-two\n-three\n+TWO\n+THREE\n+extra\n four\n five\n", "@@ -1,5 +1,6 @@"},
		{"range given backwards", 4, 2, " one\n-two\n-three\n+TWO\n+THREE\n+extra\n four\n five\n", "@@ -1,5 +1,6 @@"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := BuildLinePatch(diffReplace, c.from, c.to, false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(p, "diff --git a/a.txt b/a.txt\nindex 1111111..2222222 100644\n--- a/a.txt\n+++ b/a.txt\n"+c.hdr+"\n") {
				t.Errorf("header wrong:\n%s", p)
			}
			if got := p[strings.Index(p, "@@ -1")+len(c.hdr)+1:]; got != c.want {
				t.Errorf("body:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestBuildLinePatchNoChange(t *testing.T) {
	if _, err := BuildLinePatch(diffReplace, 5, 5, false); err != ErrNoLineChange {
		t.Errorf("context line: err = %v", err)
	}
	if _, err := BuildLinePatch(diffReplace, 40, 50, false); err != ErrNoLineChange {
		t.Errorf("outside: err = %v", err)
	}
	if _, err := BuildLinePatch("", 1, 1, false); err != ErrNoLineChange {
		t.Errorf("empty diff: err = %v", err)
	}
}

func TestBuildLinePatchReverse(t *testing.T) {
	// Reversed (unstage / discard): lines that are not chosen stay as they
	// are in the target, so an unchosen + line becomes context and an
	// unchosen - line is dropped.
	p, err := BuildLinePatch(diffReplace, 3, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "@@ -1,5 +1,6 @@\n one\n TWO\n+THREE\n extra\n four\n five\n"
	if got := p[strings.Index(p, "@@"):]; got != want {
		t.Errorf("body:\n%s\nwant:\n%s", got, want)
	}
}
