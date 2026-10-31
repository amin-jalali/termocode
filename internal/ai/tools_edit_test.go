package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEdit(t *testing.T) {
	text := "a := 1\nb := 2\na := 1\n"
	if got, err := ApplyEdit(text, "b := 2", "b := 3", false); err != nil || got != "a := 1\nb := 3\na := 1\n" {
		t.Errorf("unique = %q, %v", got, err)
	}
	if _, err := ApplyEdit(text, "a := 1", "x", false); err == nil || !strings.Contains(err.Error(), "2 places") {
		t.Errorf("ambiguous: err = %v", err)
	}
	if got, err := ApplyEdit(text, "a := 1", "x", true); err != nil || got != "x\nb := 2\nx\n" {
		t.Errorf("replace_all = %q, %v", got, err)
	}
	if _, err := ApplyEdit(text, "c := 9", "x", false); err == nil || !strings.Contains(err.Error(), "read the file again") {
		t.Errorf("stale: err = %v", err)
	}
	if _, err := ApplyEdit(text, "", "x", false); err == nil {
		t.Error("empty old_text was accepted")
	}
	if _, err := ApplyEdit(text, "b := 2", "b := 2", false); err == nil {
		t.Error("an edit that changes nothing was accepted")
	}
	// CRLF files: the model sends \n; the edit still lands and keeps \r\n.
	if got, err := ApplyEdit("x\r\ny\r\n", "x\ny", "x\nz", false); err != nil || got != "x\r\nz\r\n" {
		t.Errorf("crlf = %q, %v", got, err)
	}
}

func TestEditFileTool(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}
	p := filepath.Join(dir, "m.go")
	writeFile(t, p, "package m\n\nfunc F() int { return 1 }\n")
	tool := wsTool(t, w, "edit_file")
	if !tool.Mutating || tool.Class != ClassFileWrite {
		t.Errorf("gate = %v %q, want mutating fileWrite", tool.Mutating, tool.Class)
	}
	args := map[string]any{"path": "m.go", "old_text": "return 1", "new_text": "return 2"}
	if got := tool.Describe(args); got != "Edit m.go (1 line → 1 line)" {
		t.Errorf("describe = %q", got)
	}
	if out, err := tool.Run(t.Context(), args); err != nil || !strings.Contains(out, "edited m.go") {
		t.Fatalf("run = %q, %v", out, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "package m\n\nfunc F() int { return 2 }\n" {
		t.Errorf("file = %q", b)
	}
	// The same call again is stale now: nothing is written.
	if _, err := tool.Run(t.Context(), args); err == nil {
		t.Error("a stale edit was applied")
	}
	if err := tool.Validate(map[string]any{"path": "../x"}); err == nil {
		t.Error("a path outside the workspace passed Validate")
	}
	writeFile(t, filepath.Join(dir, ".netrc"), "machine x password 1\n")
	if _, err := tool.Run(t.Context(), map[string]any{"path": ".netrc", "old_text": "1", "new_text": "2"}); err == nil {
		t.Error("a secret file was edited")
	}
}
