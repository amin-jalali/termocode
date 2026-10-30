package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// corePick selects id in an open picker of kind.
func corePick(m Model, kind pickerKindEnum, id string) Model {
	m.pickerOpen, m.pickerKind = true, kind
	m, _ = m.update(picker.SelectMsg{ID: id})
	return m
}

// File pickers: an image opens the preview, a binary warns, text opens
// in the editor at the right line for problems and symbols.
func TestCovCorePickerSelections(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	_ = os.WriteFile(src, []byte("package main\n\nfunc main() {\n\tprintln(1)\n}\n"), 0o644)
	bin := filepath.Join(dir, "data.bin")
	_ = os.WriteFile(bin, []byte{0, 0, 1, 2, 0, 0, 3}, 0o644)

	for _, kind := range []pickerKindEnum{pickerKindFiles, pickerKindRecentFiles} {
		m.toast = covEmptyToast()
		m = corePick(m, kind, bin)
		if !strings.Contains(toastText(m), "Unsupported file") {
			t.Errorf("kind %v: binary gave no warning:\n%s", kind, toastText(m))
		}
		m = corePick(m, kind, src)
		covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0)`, src)
		if m.focus != FocusEditor {
			t.Errorf("kind %v: focus %v", kind, m.focus)
		}
		png := filepath.Join(dir, "pic.png")
		_ = os.WriteFile(png, covTinyPNG, 0o644)
		m = corePick(m, kind, png)
		if !m.imageOpen {
			t.Errorf("kind %v: image did not open the preview", kind)
		}
		m.imageOpen = false
	}

	m.problemsIndex = map[string]nvim.Diagnostic{"p-1": {Path: src, Line: 4, Col: 2}, "p-2": {}}
	m = corePick(m, pickerKindProblems, "p-1")
	covWaitLua(t, m, `return vim.fn.line('.') .. ':' .. vim.fn.col('.')`, "4:2")
	m = corePick(m, pickerKindProblems, "p-2")

	m.symbolsIndex = map[string][2]int{"s-1": {3, 1}}
	m = corePick(m, pickerKindSymbols, "s-1")
	covWaitLua(t, m, `return tostring(vim.fn.line('.'))`, "3")

	m.workspaceSymIndex = map[string]workspaceSymbolTarget{"w-1": {File: src, Line: 5, Col: 1}}
	m = corePick(m, pickerKindWorkspaceSymbols, "w-1")
	covWaitLua(t, m, `return tostring(vim.fn.line('.'))`, "5")

	m = corePick(m, pickerKindCodeAction, "no-such-action")

	// The guide picker opens the page; a bad id does nothing.
	m.pickerOpen, m.pickerKind = true, pickerKindHelpGuide
	m, cmd := m.update(picker.SelectMsg{ID: "help:reference/keys.md"})
	m = helpFeed(t, m, cmd)
	if !m.previewOpen {
		t.Error("guide page did not open")
	}
	m.previewOpen = false
	m = corePick(m, pickerKindHelpGuide, "keys.md")
	if m.previewOpen {
		t.Error("a bad guide id opened something")
	}

	// Remove Folder: the primary folder cannot go.
	m = corePick(m, pickerKindRemoveRoot, m.explorer.PrimaryRoot())
	if !strings.Contains(toastText(m), "Primary folder cannot be removed") {
		t.Errorf("remove primary:\n%s", toastText(m))
	}
	m = corePick(m, pickerKindRemoveRoot, filepath.Join(dir, "not-a-root"))
	if !strings.Contains(toastText(m), "Folder not in workspace") {
		t.Errorf("remove unknown:\n%s", toastText(m))
	}
	m = corePick(m, pickerKindRemoveRoot, "")

	// Settings rows from a picker open their value prompt.
	m = corePick(m, pickerKindSettings, "setting-tab-size")
	if !m.promptOpen {
		t.Error("settings row did not open the prompt")
	}
	m.promptOpen = false
	// A setting choice is saved.
	m.settingChoiceRow = "setting-icons"
	m = corePick(m, pickerKindSettingChoice, "ascii")
	if got := loadSettings().Icons; got != "ascii" {
		t.Errorf("icons = %q", got)
	}
	// An unknown kind ends quietly.
	m = corePick(m, pickerKindEnum(9999), "x")
}

func covEmptyToast() toast.Model { return toast.New() }

// covTinyPNG is a 2×2 PNG.
var covTinyPNG = func() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}()
