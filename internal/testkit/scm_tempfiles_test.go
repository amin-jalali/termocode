package testkit

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// privateTmp points TMPDIR at a new empty dir, so the test can see every
// temp file the app makes. Call it before the app starts.
func privateTmp(t *testing.T) string {
	t.Helper()
	dir := looseTempDir(t)
	t.Setenv("TMPDIR", dir)
	return dir
}

// diffTempFiles lists the diff temp files the app left in dir.
func diffTempFiles(dir string) []string {
	var out []string
	for _, pat := range []string{"termocode-diff-*", "termocode-commit-*"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		out = append(out, m...)
	}
	return out
}

// Spec: Given the app runs without nvim and main.go is changed, when the
// user clicks main.go in CHANGES (side-by-side diff) and a commit in GRAPH
// (commit diff), then no diff temp file is left behind.
func TestSCMDiffLeavesNoTempFileWithoutNvim(t *testing.T) {
	var tmp string
	d, _ := scmDriver(t, func(r *repo) {
		tmp = privateTmp(t)
		r.write("main.go", "package main\n// edit\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	scmClickText(d, "main.go")
	d.Settle(200 * time.Millisecond)
	if left := diffTempFiles(tmp); len(left) > 0 {
		t.Errorf("file diff left temp files: %v", left)
	}
	g := section(d, "GRAPH")
	if len(g) == 0 || !strings.Contains(g[len(g)-1], "init") {
		t.Fatalf("no init commit in GRAPH: %q", g)
	}
	scmClickText(d, "init")
	d.Settle(200 * time.Millisecond)
	if left := diffTempFiles(tmp); len(left) > 0 {
		t.Errorf("commit diff left temp files: %v", left)
	}
}

// Spec: Given main.go is changed, when the user opens its side-by-side
// diff in nvim and then a commit's diff, then both views show and no diff
// temp file is left behind.
func TestSCMNvimDiffLeavesNoTempFile(t *testing.T) {
	var tmp string
	d, _ := scmNvimDriver(t, func(r *repo) {
		tmp = privateTmp(t)
		r.write("main.go", "package main\n\nfunc main() { println(\"new\") }\n")
	})
	scmClickText(d, "main.go")
	if !waitText(d, "working") {
		t.Fatalf("no diff view:%s", dump(d))
	}
	if left := diffTempFiles(tmp); len(left) > 0 {
		t.Errorf("side-by-side diff left temp files: %v", left)
	}
	scmClickText(d, "init")
	if !waitText(d, "· commit") {
		t.Fatalf("no commit diff view:%s", dump(d))
	}
	if left := diffTempFiles(tmp); len(left) > 0 {
		t.Errorf("commit diff left temp files: %v", left)
	}
}
