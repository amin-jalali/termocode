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

const diffTwoHunks = `diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,4 @@
 l1
+new1
+new2
 l2
 l3
@@ -20,3 +21,3 @@
 l20
-l21
+L21
 l22
`

func TestBuildLinePatchTwoHunks(t *testing.T) {
	// Choosing only the second hunk drops the first one.
	p, err := BuildLinePatch(diffTwoHunks, 22, 22, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(p, "@@") != 2 || !strings.Contains(p, "@@ -20,3 +20,3 @@") {
		t.Errorf("only the second hunk, start moved back by the dropped adds:\n%s", p)
	}
	// Choosing one added line in the first hunk and the line in the second
	// moves the second hunk's new start by one, not two.
	p, err = BuildLinePatch(diffTwoHunks, 2, 22, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "@@ -1,3 +1,5 @@") || !strings.Contains(p, "@@ -20,3 +22,3 @@") {
		t.Errorf("hunk starts:\n%s", p)
	}
}

func TestBuildLinePatchRemovalAtEnd(t *testing.T) {
	d := "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,3 +1,1 @@\n a\n-b\n-c\n"
	p, err := BuildLinePatch(d, 1, 1, false)
	if err != nil {
		t.Fatalf("the last line must choose the removed tail: %v", err)
	}
	if !strings.Contains(p, "\n-b\n-c\n") {
		t.Errorf("patch:\n%s", p)
	}
}
