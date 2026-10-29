package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/explorer"
)

func explorerExpModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := aiTestModel(t)
	dir := t.TempDir()
	t.Chdir(dir)
	for name, body := range map[string]string{"a.txt": "alpha", "b.txt": "bravo"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.explorer = explorer.New()
	m.explorer.SetSize(30, 40)
	m.w = 150
	m.h = 42
	m.explorer.SetTerminalWidth(150)
	m.explorer.ToggleSelection(filepath.Join(dir, "a.txt"))
	m.explorer.ToggleSelection(filepath.Join(dir, "b.txt"))
	return &m, dir
}
func TestExplorerBulkRenameApplyUndo(t *testing.T) {
	m, dir := explorerExpModel(t)
	m.renameExplorerSelection()
	if !m.promptOpen || m.prompt.Value() != "a.txt\nb.txt" {
		t.Fatal(m.prompt.Value())
	}
	m.handlePromptSubmit("b.txt\na.txt")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatal("unsafe card")
	}
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || cmd().(confirm.SelectMsg).ID != "cancel" {
		t.Fatal("cancel not focused")
	}
	m.handleConfirmSelect("cancel")
	if readFile(filepath.Join(dir, "a.txt")) != "alpha" {
		t.Fatal("cancel changed disk")
	}
	m.handleConfirmSelect("apply")
	if readFile(filepath.Join(dir, "a.txt")) != "bravo" || readFile(filepath.Join(dir, "b.txt")) != "alpha" {
		t.Fatal("swap failed")
	}
	if len(m.explorer.Selection()) != 0 || !strings.HasSuffix(m.explorer.CursorPath(), "b.txt") {
		t.Fatal("tree not refreshed")
	}
	m.undoBulkRename(m.bulkRename.serial)
	if readFile(filepath.Join(dir, "a.txt")) != "alpha" || readFile(filepath.Join(dir, "b.txt")) != "bravo" {
		t.Fatal("undo failed")
	}
}
func TestExplorerBulkRenameConflictsAndChanges(t *testing.T) {
	for _, tt := range []struct{ name, value string }{{"duplicate", "same.txt\nsame.txt"}, {"empty", "\nb.txt"}, {"slash", "a/x\nb.txt"}, {"line count", "a.txt"}} {
		t.Run(tt.name, func(t *testing.T) {
			m, dir := explorerExpModel(t)
			m.renameExplorerSelection()
			m.previewBulkRename(tt.value)
			if len(m.bulkRename.plan.Conflicts) == 0 || !strings.Contains(m.confirm.Box(), "Conflict") {
				t.Fatal(m.confirm.Box())
			}
			m.handleConfirmSelect("apply")
			if readFile(filepath.Join(dir, "a.txt")) != "alpha" {
				t.Fatal("conflict applied")
			}
		})
	}
	t.Run("changed before apply", func(t *testing.T) {
		m, dir := explorerExpModel(t)
		m.renameExplorerSelection()
		m.previewBulkRename("c.txt\nd.txt")
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		m.applyBulkRename()
		if readFile(filepath.Join(dir, "c.txt")) != "<missing>" {
			t.Fatal("changed file renamed")
		}
	})
	t.Run("changed before undo", func(t *testing.T) {
		m, dir := explorerExpModel(t)
		m.renameExplorerSelection()
		m.previewBulkRename("c.txt\nd.txt")
		m.applyBulkRename()
		if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		m.undoBulkRename(m.bulkRename.serial)
		if readFile(filepath.Join(dir, "c.txt")) != "changed" || readFile(filepath.Join(dir, "a.txt")) != "<missing>" {
			t.Fatal("changed file undone")
		}
	})
	t.Run("undo target exists", func(t *testing.T) {
		m, dir := explorerExpModel(t)
		m.renameExplorerSelection()
		m.previewBulkRename("c.txt\nd.txt")
		m.applyBulkRename()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		m.undoBulkRename(m.bulkRename.serial)
		if readFile(filepath.Join(dir, "a.txt")) != "new" {
			t.Fatal("undo overwrote new file")
		}
	})
}
func TestExplorerBulkRenameBuffersFollowSwap(t *testing.T) {
	m, dir := explorerExpModel(t)
	c := aiTestNvim(t, m)
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := c.ExecLuaArgs(`vim.cmd.edit(vim.fn.fnameescape(...))`, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	// Unsaved text must follow its original file through the swap.
	if err := c.ExecLuaArgs(`local p=...;for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_get_name(b)==p then vim.api.nvim_buf_set_lines(b,0,-1,false,{'unsaved alpha'}) end end`, filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m.renameExplorerSelection()
	m.previewBulkRename("b.txt\na.txt")
	m.applyBulkRename()
	if got := bufText(t, c, filepath.Join(dir, "b.txt")); got != "unsaved alpha" {
		t.Fatal(got)
	}
	if got := bufText(t, c, filepath.Join(dir, "a.txt")); got != "bravo" {
		t.Fatal(got)
	}
	m.undoBulkRename(m.bulkRename.serial)
	if got := bufText(t, c, filepath.Join(dir, "a.txt")); got != "unsaved alpha" {
		t.Fatal(got)
	}
	m.explorer.ToggleSelection(filepath.Join(dir, "a.txt"))
	m.explorer.ToggleSelection(filepath.Join(dir, "b.txt"))
	m.renameExplorerSelection()
	m.previewBulkRename("c.txt\nd.txt")
	m.applyBulkRename()
	if err := c.ExecLuaArgs(`local p=...;for _,b in ipairs(vim.api.nvim_list_bufs()) do if vim.api.nvim_buf_get_name(b)==p then vim.api.nvim_buf_set_lines(b,0,-1,false,{'new edit'}) end end`, filepath.Join(dir, "c.txt")); err != nil {
		t.Fatal(err)
	}
	m.undoBulkRename(m.bulkRename.serial)
	if readFile(filepath.Join(dir, "c.txt")) != "alpha" || readFile(filepath.Join(dir, "a.txt")) != "<missing>" {
		t.Fatal("undo ignored unsaved edits")
	}
}
func TestExplorerPreviewLayoutToggle(t *testing.T) {
	m, _ := explorerExpModel(t)
	original := 30
	m.explorerWidth = original
	m.toggleExplorerPreview()
	if !m.explorer.PreviewVisible() || m.explorerWidth <= original {
		t.Fatal("preview not shown")
	}
	m.w = 99
	m.applyLayout()
	if m.explorer.PreviewVisible() || m.explorerWidth != original {
		t.Fatal("narrow preview")
	}
	m.w = 150
	m.applyLayout()
	if !m.explorer.PreviewVisible() {
		t.Fatal("resize preview")
	}
	m.toggleExplorerPreview()
	if m.explorerWidth != original || m.explorer.PreviewEnabled() {
		t.Fatal("restore width")
	}
}
