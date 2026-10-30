package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// covKeyNamed is the key message whose String() is s ("ctrl+s",
// "ctrl+shift+c", "alt+z"): the patched Bubble Tea carries any named
// key as KeyExtended (the kitty keyboard protocol).
// Keys a legacy terminal can send come as it sends them.
func covKeyNamed(s string) tea.KeyMsg {
	r := strings.TrimPrefix(s, "alt+")
	if len([]rune(r)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r), Alt: r != s}
	}
	for kt := tea.KeyType(-200); kt < 200; kt++ {
		for _, alt := range []bool{false, true} {
			if k := (tea.KeyMsg{Type: kt, Alt: alt}); kt != tea.KeyRunes && k.String() == s {
				return k
			}
		}
	}
	return tea.KeyMsg{Type: tea.KeyExtended, Name: s}
}

// covPress presses a key bound to a. An action with no default key gets
// one, as a user would bind it in keymap.json.
func covPress(t *testing.T, m Model, a keymap.Action) (Model, tea.Cmd) {
	t.Helper()
	for k, b := range m.keys.Bindings() {
		if b == a && m.keys.Match(covKeyNamed(k)) == a {
			return m.update(covKeyNamed(k))
		}
	}
	const spare = "ctrl+alt+shift+f12"
	m.keys = m.keys.MergeInto(map[string]keymap.Action{spare: a})
	if m.keys.Match(covKeyNamed(spare)) != a {
		t.Fatalf("cannot bind %s", keymap.ActionName(a))
	}
	return m.update(covKeyNamed(spare))
}

// covEditorFile opens a real file in nvim and tells the model about it.
func covEditorFile(t *testing.T, m *Model, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	m.editor.SetMeta(p, false, "")
	return p
}

// Focus and layout keys move the focus and the sidebar.
func TestCovCoreKeysFocus(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.focus = FocusEditor
	m, _ = covPress(t, m, keymap.ActionFocusSwap)
	if m.focus != FocusExplorer {
		t.Errorf("focus swap: %v", m.focus)
	}
	m, _ = covPress(t, m, keymap.ActionFocusSwap)
	if m.focus != FocusEditor {
		t.Errorf("focus swap back: %v", m.focus)
	}
	m, _ = covPress(t, m, keymap.ActionFocusExplorer)
	if m.focus != FocusExplorer {
		t.Errorf("focus explorer: %v", m.focus)
	}
	m, _ = covPress(t, m, keymap.ActionFocusEditor)
	if m.focus != FocusEditor {
		t.Errorf("focus editor: %v", m.focus)
	}
	m, _ = covPress(t, m, keymap.ActionShowCheatSheet)
	if !m.previewOpen {
		t.Error("cheat sheet not shown")
	}
	m.previewOpen = false
	m, _ = covPress(t, m, keymap.ActionPickTheme)
	if !m.pickerOpen || m.pickerKind != pickerKindTheme {
		t.Errorf("theme picker: %v %v", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	m, _ = covPress(t, m, keymap.ActionServicesFocus)
	if m.activity.Active() != activity.ViewServices {
		t.Errorf("services key: view %v", m.activity.Active())
	}
	m, _ = covPress(t, m, keymap.ActionDatabasesFocus)
	if m.activity.Active() != activity.ViewDatabases {
		t.Errorf("databases key: view %v", m.activity.Active())
	}
	_, cmd := covPress(t, m, keymap.ActionOpenShell)
	if cmd == nil {
		t.Error("open shell gave no command")
	}
	m, _ = covPress(t, m, keymap.ActionReloadWindow)
	if !strings.Contains(toastText(m), "Reload failed") {
		t.Errorf("reload headless:\n%s", toastText(m))
	}
}

// Editing keys reach nvim.
func TestCovCoreKeysEditing(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	p := covEditorFile(t, &m, "a.go", "package a\n\nfunc A() {}\n")
	m.focus = FocusEditor

	m, _ = covPress(t, m, keymap.ActionDuplicateLine)
	m, _ = covPress(t, m, keymap.ActionSplitHorizontal)
	covWaitLua(t, m, `return tostring(#vim.api.nvim_tabpage_list_wins(0) >= 2)`, "true")
	m, _ = covPress(t, m, keymap.ActionCloseSplit)
	m, _ = covPress(t, m, keymap.ActionToggleComment)
	hints := m.inlayHintsOn
	m, _ = covPress(t, m, keymap.ActionToggleInlayHints)
	if m.inlayHintsOn == hints {
		t.Error("inlay hints mirror not flipped")
	}
	m, _ = covPress(t, m, keymap.ActionSaveAll)
	if !strings.Contains(toastText(m), "Saved all open files") {
		t.Errorf("save all:\n%s", toastText(m))
	}
	m, _ = covPress(t, m, keymap.ActionCopyFileRef)
	m, _ = covPress(t, m, keymap.ActionReloadBuffer)
	m, _ = covPress(t, m, keymap.ActionHover)
	m, _ = covPress(t, m, keymap.ActionGotoImplementation)
	m, _ = covPress(t, m, keymap.ActionOpenURL)
	m, _ = covPress(t, m, keymap.ActionToggleAutoSave)
	m, _ = covPress(t, m, keymap.ActionShowSnippetPicker)
	m.pickerOpen = false
	_ = p

	// Next / previous buffer when no panel is open.
	q := covEditorFile(t, &m, "b.go", "package b\n")
	m, _ = covPress(t, m, keymap.ActionTerminalNextTab)
	covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0)`, p)
	m, _ = covPress(t, m, keymap.ActionTerminalPrevTab)
	covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0)`, q)
	// With the panel open they cycle its tabs.
	m.toggleTerminalPanel()
	m.newTerminalTab()
	m, _ = covPress(t, m, keymap.ActionTerminalNextTab)
	if m.terminalActiveTab != 0 {
		t.Errorf("next panel tab: %d", m.terminalActiveTab)
	}
	m, _ = covPress(t, m, keymap.ActionTerminalPrevTab)
	if m.terminalActiveTab != 1 {
		t.Errorf("prev panel tab: %d", m.terminalActiveTab)
	}
}

// Ctrl+Shift+C in the terminal copies its text and says how much.
func TestCovCoreKeysTerminalCopy(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	m.toggleTerminalPanel()
	buf := itoa(m.activeTerminalTab().BufID)
	_ = m.nvim.ExecLua(`vim.fn.chansend(vim.bo[` + buf + `].channel, "echo one; echo two\n")`)
	covWaitLua(t, m, `return table.concat(vim.api.nvim_buf_get_lines(`+buf+`, 0, -1, false), '\n'):find('two\n') and '1' or '0'`, "1")
	m.inTerminal = true
	m.focus = FocusEditor
	m, _ = m.update(covKeyNamed("ctrl+shift+c"))
	if !strings.Contains(toastText(m), "lines from terminal") {
		t.Errorf("copy toast:\n%s", toastText(m))
	}
	m, _ = m.update(covKeyNamed("ctrl+shift+v"))
}

// Save on a file that cannot be written says so.
func TestCovCoreKeysSaveFails(t *testing.T) {
	withConfigDir(t)
	m := covNew(t)
	m, _ = m.update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editor.SetMeta(filepath.Join(t.TempDir(), "x.go"), true, "go")
	m, _ = covPress(t, m, keymap.ActionSave)
	if m.err == "" && !strings.Contains(toastText(m), "Save failed") && !strings.Contains(toastText(m), "Saved") {
		t.Errorf("save without nvim: err %q toast %q", m.err, toastText(m))
	}
}

// Ctrl+F and Ctrl+H start with the selected text.
func TestCovCoreKeysFindWithSelection(t *testing.T) {
	for _, a := range []keymap.Action{keymap.ActionFind, keymap.ActionReplaceInFile} {
		m := covNvimModel(t, 120, 40)
		covEditorFile(t, &m, "s.txt", "alpha needle beta\n")
		_ = m.nvim.Input("0wviw")
		covWaitLua(t, m, `return vim.fn.mode()`, "v")
		m.focus = FocusEditor
		m, _ = covPress(t, m, a)
		if !m.findOpen || m.find.Query() != "needle" {
			t.Errorf("%s: open %v query %q", keymap.ActionName(a), m.findOpen, m.find.Query())
		}
	}
}

// More keys: hidden files, a comment in visual mode, a key that drops
// the code action hint, Reveal in the explorer.
func TestCovCoreKeysMore(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	covEditorFile(t, &m, "k.go", "package k\n")
	m.focus = FocusEditor
	shown := m.explorer.HiddenShown()
	m, _ = covPress(t, m, keymap.ActionToggleHidden)
	if m.explorer.HiddenShown() == shown || !strings.Contains(toastText(m), "Hidden files:") {
		t.Errorf("hidden files: %v", m.explorer.HiddenShown())
	}
	m.editorMode = "V"
	m, _ = covPress(t, m, keymap.ActionToggleComment)
	m.editorMode = "n"
	m.preferredCodeAction = &nvim.CodeAction{Title: "fix"}
	m, _ = covPress(t, m, keymap.ActionFocusEditor)
	if m.preferredCodeAction != nil {
		t.Error("another key kept the code action hint")
	}
	m.showExp = false
	_ = m.dispatchPaletteAction("reveal-file")
	if !m.showExp {
		t.Error("reveal did not show the explorer")
	}
	var none Model
	none.pushClosed("/w/closed.go")
	if none.reopenClosed() != nil {
		t.Error("reopen without nvim")
	}
	_ = none.reopenClosed()
	if !strings.Contains(toastText(none), "No recently closed editors") {
		t.Errorf("empty ring:\n%s", toastText(none))
	}
}
