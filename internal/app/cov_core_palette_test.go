package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Palette commands that act on nvim do what their titles say.
func TestCovCorePaletteEditorCommands(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	p := covEditorFile(t, &m, "p.txt", "one\ntwo\n")
	lines := func() string { return covBufText(t, m) }

	_ = m.dispatchPaletteAction("duplicate-line") // a key of termocode's own nvim setup
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'one', 'two', 'three'})`)
	_ = m.dispatchPaletteAction("fold-all")
	covWaitLua(t, m, `return tostring(vim.wo.foldlevel)`, "0")
	_ = m.dispatchPaletteAction("unfold-all")
	covWaitLua(t, m, `return tostring(vim.wo.foldlevel)`, "99")
	_ = m.dispatchPaletteAction("toggle-fold")
	_ = m.dispatchPaletteAction("split-vertical")
	_ = m.dispatchPaletteAction("split-horizontal")
	covWaitLua(t, m, `return tostring(#vim.api.nvim_tabpage_list_wins(0))`, "3")
	_ = m.dispatchPaletteAction("close-split")
	covWaitLua(t, m, `return tostring(#vim.api.nvim_tabpage_list_wins(0))`, "2")
	_ = m.dispatchPaletteAction("save")
	if b, _ := os.ReadFile(p); strings.Count(string(b), "\n") != 3 {
		t.Errorf("save: file %q (buffer %q)", b, lines())
	}
	_ = m.dispatchPaletteAction("code-action-apply-preferred")

	// Close: a clean buffer closes, and Reopen Closed brings it back.
	q := covEditorFile(t, &m, "q.txt", "q\n")
	id, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_buf())`)
	m.activeBuf = parseInt(id)
	m.bufs = append(m.bufs, nvimBuf(parseInt(id), q))
	_ = m.dispatchPaletteAction("close-buffer")
	covWaitLua(t, m, `return vim.fn.buflisted(`+id+`) == 1 and '1' or '0'`, "0")
	_ = m.dispatchPaletteAction("prev-buffer")
	// A dirty buffer asks first.
	covModify(t, m, p, "changed")
	id, _ = m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_buf())`)
	m.activeBuf = parseInt(id)
	_ = m.dispatchPaletteAction("close-buffer")
	if !m.confirmOpen || m.confirmKind != confirmKindCloseBuffer {
		t.Error("closing a dirty buffer did not ask")
	}
	m.confirmOpen = false

	// Panel commands with the terminal open.
	m.toggleTerminalPanel()
	m.newTerminalTab()
	_ = m.dispatchPaletteAction("terminal-next-tab")
	if m.terminalActiveTab != 0 {
		t.Errorf("terminal-next-tab: %d", m.terminalActiveTab)
	}
	_ = m.dispatchPaletteAction("terminal-maximize")
	if m.terminalRows != m.h-8 {
		t.Errorf("terminal-maximize: %d", m.terminalRows)
	}
	_ = m.dispatchPaletteAction("dap-install")
	if !m.pickerOpen || m.pickerKind != pickerKindToolManager {
		t.Error("dap-install did not open the tool manager")
	}
}

// Clipboard history: empty opens nothing; with entries a pick pastes.
func TestCovCoreClipboardHistory(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	covEditorFile(t, &m, "c.txt", "\n")
	if m.openClipboardPicker() != nil || m.pickerOpen {
		t.Error("an empty history opened a picker")
	}
	m.clipRing.Push("first copy")
	m.clipRing.Push("second copy")
	_ = m.dispatchPaletteAction("clipboard-history")
	if !m.pickerOpen || m.pickerKind != pickerKindClipboard {
		t.Fatal("no clipboard picker")
	}
	var id string
	for _, it := range m.clipboardPickerItems() {
		if it.Content == "first copy" {
			id = "clip-" + itoa(it.Index)
		}
	}
	m, _ = m.update(picker.SelectMsg{ID: id})
	covWaitLua(t, m, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n'):find('first copy') and '1' or '0'`, "1")
	// A bad row id pastes nothing.
	m.pickerKind = pickerKindClipboard
	m, _ = m.update(picker.SelectMsg{ID: "clip-99"})
	m.pickerKind = pickerKindClipboard
	m, _ = m.update(picker.SelectMsg{ID: "nonsense"})
}

// Quick Open lists the files of every root once, skips hidden and build
// folders, and names extra roots' files after their root.
func TestCovCoreLoadFilesIn(t *testing.T) {
	main := t.TempDir()
	extra := t.TempDir()
	for _, f := range []string{"a.go", "sub/b.go", ".git/HEAD", "node_modules/x.js", ".hidden/c.go"} {
		p := filepath.Join(main, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o644)
	}
	_ = os.WriteFile(filepath.Join(extra, "e.go"), nil, 0o644)
	items := loadFilesIn([]string{main, extra, main, filepath.Join(main, "missing")})
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	sort.Strings(titles)
	want := []string{"a.go", filepath.Join(filepath.Base(extra), "e.go"), filepath.Join("sub", "b.go")}
	sort.Strings(want)
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Errorf("titles = %q, want %q", titles, want)
	}
	for _, it := range items {
		if strings.HasSuffix(it.Title, "e.go") && !filepath.IsAbs(it.ID) {
			t.Errorf("extra root id not absolute: %q", it.ID)
		}
	}
}

// The workspace roots: the folder first, then saved roots that exist.
func TestCovCoreWorkspaceRoots(t *testing.T) {
	withConfigDir(t)
	cwd, err := procRootDir()
	if err != nil {
		t.Skip("no workspace folder")
	}
	extra := t.TempDir()
	s := loadSession()
	s.Roots = []string{"", cwd, filepath.Join(extra, "gone"), extra}
	saveSession(s)
	got := workspaceRoots()
	if len(got) != 2 || got[0] != cwd || got[1] != extra {
		t.Errorf("roots = %q", got)
	}
}

func nvimBuf(id int, path string) nvim.BufferInfo { return nvim.BufferInfo{ID: id, Path: path} }
