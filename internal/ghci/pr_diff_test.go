package ghci

import (
	"context"
	"strings"
	"testing"
)

const prDiffText = `diff --git a/parser.go b/parser.go
index 111..222 100644
--- a/parser.go
+++ b/parser.go
@@ -1,3 +1,4 @@
 package p
-func Old() {}
+func New() {}
+func Extra() {}
 var x = 1
@@ -20,2 +21,2 @@
 end
-bye
+ciao
diff --git a/docs/new.md b/docs/new.md
new file mode 100644
--- /dev/null
+++ b/docs/new.md
@@ -0,0 +1,2 @@
+# Title
+text
diff --git a/old.txt b/renamed.txt
similarity index 90%
rename from old.txt
rename to renamed.txt
diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1 +0,0 @@
-package gone
`

func TestParseDiff(t *testing.T) {
	fs := ParseDiff(prDiffText)
	if len(fs) != 5 {
		t.Fatalf("files %d: %+v", len(fs), fs)
	}
	p := fs[0]
	if p.Path != "parser.go" || p.Add != 3 || p.Del != 2 {
		t.Errorf("parser %+v", p)
	}
	if p.Old != "package p\nfunc Old() {}\nvar x = 1\n…\nend\nbye\n" || p.New != "package p\nfunc New() {}\nfunc Extra() {}\nvar x = 1\n…\nend\nciao\n" {
		t.Errorf("old %q new %q", p.Old, p.New)
	}
	if !fs[1].Created || fs[1].Old != "" || fs[1].New != "# Title\ntext\n" {
		t.Errorf("new file %+v", fs[1])
	}
	if fs[2].Path != "renamed.txt" || fs[2].OldPath != "old.txt" {
		t.Errorf("rename %+v", fs[2])
	}
	if !fs[3].Binary || !fs[4].Deleted || fs[4].Old != "package gone\n" || fs[4].New != "" {
		t.Errorf("binary/deleted %+v %+v", fs[3], fs[4])
	}
}

func TestParseDiffEmpty(t *testing.T) {
	if got := ParseDiff(""); len(got) != 0 {
		t.Errorf("empty diff gave %+v", got)
	}
}

func TestPRDiff(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr diff": {out: prDiffText}}}
	fs, err := PRDiff(context.Background(), f.run, "/repo", 14)
	if err != nil || len(fs) != 5 {
		t.Fatalf("%v %d", err, len(fs))
	}
	if got := strings.Join(f.calls[0], " "); got != "pr diff 14" {
		t.Errorf("args %q", got)
	}
}
