package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/codeindex"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// codebaseTree is a small workspace with a secret file, its cache kept
// in a temp folder.
func codebaseTree(t *testing.T) string {
	t.Helper()
	t.Setenv(codeindex.CacheDirEnv, t.TempDir())
	root := t.TempDir()
	for p, s := range map[string]string{
		"auth/login.go":    "package auth\n\n// CheckPassword compares the typed password with the stored hash.\nfunc CheckPassword(u, p string) bool { return false }\n",
		"store/remove.go":  "package store\n\n// DeleteRow drops one row from the table.\nfunc DeleteRow(id int) error { return nil }\n",
		".git-credentials": "https://user:password@example.com\n",
	} {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Spec (CMP-69): codebase_search is a read-only AI tool that lists the
// best chunks as file:line with a score, and never a secret file.
func TestCodebaseSearchTool(t *testing.T) {
	root := codebaseTree(t)
	tool := codebaseSearchTool(root)
	if !tool.ReadOnly || tool.Mutating || tool.Gate != nil {
		t.Fatalf("codebase_search must be read-only: %+v", tool)
	}
	out, err := tool.Run(context.Background(), map[string]any{"query": "sign in credentials"})
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(first, "auth/login.go:") {
		t.Fatalf("first hit %q, want auth/login.go; out:\n%s", first, out)
	}
	if strings.Contains(out, ".git-credentials") {
		t.Fatalf("a secret file was indexed:\n%s", out)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"query": " "}); err == nil {
		t.Fatal("an empty query is not refused")
	}
}

// Spec (CMP-69): the tool is in the chat's tool list.
func TestCodebaseSearchInChatTools(t *testing.T) {
	src, err := os.ReadFile("ai_chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "codebaseSearchTool(root)") {
		t.Fatal("aiChatTools does not add codebase_search")
	}
}

// Spec (CMP-69): Search: Codebase by Meaning… is a palette command the
// AI may not run (it opens a prompt; the AI has the typed tool).
func TestCodebaseSearchPalette(t *testing.T) {
	found := false
	for _, it := range paletteItems() {
		if it.ID == codebaseSearchID {
			found = strings.HasPrefix(it.Title, "Search: ")
		}
	}
	if !found {
		t.Fatal("no Search: palette item for codebase-search")
	}
	p, ok := aiActionPolicies[codebaseSearchID]
	if !ok || p.Gate != aiGateNever || !strings.Contains(p.Reason, "codebase_search") {
		t.Fatalf("policy = %+v", p)
	}
}

// Spec (CMP-69): the palette command asks for words, the search runs off
// the UI loop, and Enter on a hit jumps to its file and line.
func TestCodebaseSearchPromptToPicker(t *testing.T) {
	root := codebaseTree(t)
	m := &Model{ws: workspace.Workspace{ID: "local:" + root, Root: root}}
	m.dispatchPaletteAction(codebaseSearchID)
	if !m.promptOpen || m.promptKind != promptKindCodebaseSearch {
		t.Fatalf("prompt open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	cmd := m.handlePromptSubmit("remove a record")
	if !m.pickerOpen || cmd == nil {
		t.Fatalf("picker open=%v cmd=%v", m.pickerOpen, cmd != nil)
	}
	msg, ok := cmd().(applyMsg)
	if !ok {
		t.Fatal("search did not return an applyMsg")
	}
	msg(m)
	it, ok := m.picker.Current()
	if !ok {
		t.Fatal("no hit in the picker")
	}
	tg, ok := m.workspaceSymIndex[it.ID]
	if !ok || tg.File != filepath.Join(root, "store", "remove.go") || tg.Line < 1 {
		t.Fatalf("hit %+v -> %+v", it, tg)
	}
	if m.pickerKind != pickerKindWorkspaceSymbols {
		t.Fatalf("pickerKind = %v, want the jump picker", m.pickerKind)
	}
}

// Spec (CMP-69): an empty query does nothing.
func TestCodebaseSearchEmpty(t *testing.T) {
	root := codebaseTree(t)
	m := &Model{ws: workspace.Workspace{ID: "local:" + root, Root: root}}
	if cmd := m.onCodebaseSearch("  "); cmd != nil || m.pickerOpen {
		t.Fatal("an empty query searched")
	}
}
