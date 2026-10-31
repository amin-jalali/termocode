package ai

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchToolRegexAndGlob(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // the Go walker: same answer on every machine
	dir := t.TempDir()
	w := Workspace{Root: dir}
	writeFile(t, filepath.Join(dir, "a.go"), "func Alpha() {}\nfunc beta() {}\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "func Gamma() {}\n")

	out, err := runTool(t, w, "search", map[string]any{"query": `func [A-Z]\w+`, "regex": true})
	if err != nil || !strings.Contains(out, "a.go:1:") || !strings.Contains(out, "b.txt:1:") || strings.Contains(out, "beta") {
		t.Fatalf("regex search = %q, %v", out, err)
	}
	out, err = runTool(t, w, "search", map[string]any{"query": `func [A-Z]\w+`, "regex": true, "glob": "*.go"})
	if err != nil || !strings.Contains(out, "a.go:1:") || strings.Contains(out, "b.txt") {
		t.Fatalf("glob search = %q, %v", out, err)
	}
	if out, _ := runTool(t, w, "search", map[string]any{"query": "func [A-Z]"}); out != "no matches" {
		t.Errorf("literal search treated the query as a regex: %q", out)
	}
	if _, err := runTool(t, w, "search", map[string]any{"query": "(", "regex": true}); err == nil || !strings.Contains(err.Error(), "regex") {
		t.Errorf("bad regex: err = %v", err)
	}
}

func TestGlobToolListsFiles(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	w := Workspace{Root: dir}
	for _, p := range []string{"a.go", "sub/b_test.go", "sub/c.go"} {
		writeFile(t, filepath.Join(dir, p), "x\n")
	}
	out, err := runTool(t, w, "glob", map[string]any{"pattern": "**/*.go"})
	if err != nil || out != "a.go\nsub/b_test.go\nsub/c.go\n" {
		t.Fatalf("glob = %q, %v", out, err)
	}
	out, err = runTool(t, w, "glob", map[string]any{"pattern": "*.go", "path": "sub"})
	if err != nil || out != "sub/b_test.go\nsub/c.go\n" {
		t.Fatalf("glob in sub = %q, %v", out, err)
	}
	if out, _ := runTool(t, w, "glob", map[string]any{"pattern": "*.rs"}); out != "no files" {
		t.Errorf("none = %q", out)
	}
	if _, err := runTool(t, w, "glob", map[string]any{"pattern": "*", "path": "../x"}); err == nil {
		t.Error("a path outside the workspace was listed")
	}
	tool := wsTool(t, w, "glob")
	if tool.Mutating || tool.Describe(map[string]any{"pattern": "*.go"}) != "Find files: *.go" {
		t.Errorf("glob gate/describe wrong: %v %q", tool.Mutating, tool.Describe(map[string]any{"pattern": "*.go"}))
	}
}
