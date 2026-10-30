package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/explorer"
)

// IDE tools (ai_ide_tools.go) without the editor: open_file refusals,
// show_panel for every panel, reveal_in_explorer, notify and the focus
// names get_ide_state reports.

func covIDETools(t *testing.T) (*Model, string, map[string]ai.Tool) {
	t.Helper()
	m := aiTestModel(t)
	root := t.TempDir()
	t.Chdir(root)
	m.explorer = explorer.New()
	m.problems = newProblemsPanel()
	m.output = newOutputStore()
	tools := map[string]ai.Tool{}
	for _, tl := range aiIDETools(root, covSyncCall(&m)) {
		tools[tl.Name] = tl
	}
	return &m, root, tools
}

func TestCovAIIDEToolsWithoutEditor(t *testing.T) {
	m, root, tools := covIDETools(t)
	ctx := context.Background()
	_ = os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "blob.bin"), []byte{0, 1, 2, 0, 0, 0xff}, 0o644)
	open := tools["open_file"]
	for a, want := range map[string]string{"": "path is required", "pkg": "is a folder", "missing.go": "does not exist", "a.go": "the editor is not running"} {
		_, err := open.Run(ctx, map[string]any{"path": a})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("open %q: %v", a, err)
		}
	}
	if _, err := open.Run(ctx, map[string]any{"path": "blob.bin"}); err == nil {
		t.Error("a binary file was opened")
	}
	if _, err := tools["notify"].Run(ctx, map[string]any{"title": "  "}); err == nil {
		t.Error("an empty notification was shown")
	}
	if out, err := tools["notify"].Run(ctx, map[string]any{"title": "Done", "detail": "all tests pass"}); err != nil || out != "shown" {
		t.Errorf("notify: %q %v", out, err)
	}
	if _, err := tools["show_panel"].Run(ctx, map[string]any{"name": "nope"}); err == nil {
		t.Error("an unknown panel was shown")
	}
	m.showExp = false
	for _, name := range []string{"problems", "output", "terminal", "test-results", "explorer", "git", "search"} {
		if out, err := tools["show_panel"].Run(ctx, map[string]any{"name": name}); err != nil || out != "shown "+name {
			t.Errorf("%s: %q %v", name, out, err)
		}
	}
	if !m.showExp || m.activity.Active() != activity.ViewSearch {
		t.Errorf("sidebar shown %v view %v", m.showExp, m.activity.Active())
	}
	if out, err := tools["reveal_in_explorer"].Run(ctx, map[string]any{"path": "pkg"}); err != nil || out != "revealed pkg" {
		t.Errorf("reveal: %q %v", out, err)
	}
	if _, err := tools["reveal_in_explorer"].Run(ctx, map[string]any{"path": "nope"}); err == nil {
		t.Error("reveal of a missing path")
	}
	if got := tools["reveal_in_explorer"].Describe(map[string]any{"path": "pkg"}); got != "Reveal pkg" {
		t.Errorf("reveal card = %q", got)
	}
	ex := tools["execute_action"]
	if _, err := ex.Run(ctx, map[string]any{"id": "no-such-command"}); err == nil {
		t.Error("an unknown command ran")
	}
	if got := ex.Describe(map[string]any{"id": "no-such-command"}); got != "IDE command no-such-command" {
		t.Errorf("unknown command card = %q", got)
	}
	for focus, want := range map[string]string{"terminal": "terminal", "panel": "panel", "explorer": "explorer", "editor": "editor"} {
		m.inTerminal, m.panelFocused, m.focus = focus == "terminal", focus == "panel", FocusEditor
		if focus == "explorer" {
			m.focus = FocusExplorer
		}
		if got := m.aiFocusName(); got != want {
			t.Errorf("focus %s = %q", focus, got)
		}
	}
}
