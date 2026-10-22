package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// importFileModel opens the picker and chooses "From a file…"; the path
// prompt is then open.
func importFileModel(t *testing.T) Model {
	t.Helper()
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	m.pickerOpen = false
	cmd := m.handlePickerSelect(picker.SelectMsg{ID: importFileID, Title: importFileTitle})
	if cmd != nil {
		t.Fatalf("choosing From a file… started work: %v", cmd)
	}
	if !m.promptOpen || m.promptKind != promptKindImportFile {
		t.Fatalf("path prompt not open: open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	return m
}

// importFileSubmit sends path through the prompt and feeds the read.
func importFileSubmit(t *testing.T, m Model, path string) Model {
	t.Helper()
	next, cmd := m.Update(prompt.SubmitMsg{Value: path})
	return importPlanFeed(next.(Model), cmd)
}

// importPlanFeed runs cmd (a batch is opened) and gives every plan
// message to m.
func importPlanFeed(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	// A toast timer in the batch would sleep for seconds: give it no time.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var got tea.Msg
	select {
	case got = <-done:
	case <-time.After(150 * time.Millisecond):
		return m
	}
	switch msg := got.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = importPlanFeed(m, c)
		}
	case importPlanMsg:
		next, more := m.Update(msg)
		m = importPlanFeed(next.(Model), more)
	}
	return m
}

func importTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			out[rel] = string(b)
		}
		return nil
	})
	return out
}

func TestImportPickerEndsWithFromAFile(t *testing.T) {
	importSandbox(t, "vscode", "sublime")
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	items := pickerItems(t, &m)
	if len(items) < 3 {
		t.Fatalf("rows %v", items)
	}
	last := items[len(items)-1]
	if last.ID != importFileID || !strings.HasPrefix(last.Title, "From a file") {
		t.Errorf("last row %+v", last)
	}
	for _, it := range items[:len(items)-1] {
		if it.ID == importFileID {
			t.Errorf("From a file… is not last: %v", items)
		}
	}
}

func TestImportPickerWithoutEditorsOffersOnlyFromAFile(t *testing.T) {
	importSandbox(t)
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	if !m.pickerOpen || m.pickerKind != pickerKindImportSource {
		t.Fatal("no picker with nothing detected")
	}
	items := pickerItems(t, &m)
	if len(items) != 1 || items[0].ID != importFileID {
		t.Fatalf("rows %v", items)
	}
	if frame := ansi.Strip(m.View()); !strings.Contains(frame, "No other editor found") {
		t.Errorf("picker does not say no editor was found:\n%s", frame)
	}
}

func TestImportFromFileOpensPanelThenCancelWritesNothing(t *testing.T) {
	home, xdg := importSandbox(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "Preferences.sublime-settings")
	if err := os.WriteFile(p, []byte(`{"tab_size": 4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := importFileModel(t)
	beforeHome, beforeXDG, beforeDir := importTree(t, home), importTree(t, xdg), importTree(t, dir)
	m = importFileSubmit(t, m, p)
	if !m.importOpen {
		t.Fatalf("panel did not open: %s", importToast(m))
	}
	frame := ansi.Strip(m.View())
	for _, s := range []string{"Import from Preferences.sublime-settings", "tab_size = 4"} {
		if !strings.Contains(frame, s) {
			t.Errorf("frame lacks %q:\n%s", s, frame)
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = importFeed(t, next.(Model), cmd)
	if m.importOpen {
		t.Fatal("Esc left the panel open")
	}
	for name, pair := range map[string][2]map[string]string{
		"home": {beforeHome, importTree(t, home)}, "xdg": {beforeXDG, importTree(t, xdg)}, "file folder": {beforeDir, importTree(t, dir)},
	} {
		if len(pair[0]) != len(pair[1]) {
			t.Errorf("%s changed: %v -> %v", name, pair[0], pair[1])
		}
		for k, v := range pair[0] {
			if pair[1][k] != v {
				t.Errorf("%s: %s changed", name, k)
			}
		}
	}
}

func TestImportFromFileExpandsTilde(t *testing.T) {
	home, _ := importSandbox(t)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(`{"editor.tabSize": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := importFileSubmit(t, importFileModel(t), "~/settings.json")
	if !m.importOpen || !strings.Contains(ansi.Strip(m.View()), "editor.tabSize = 2") {
		t.Fatalf("panel not open: %s", importToast(m))
	}
}

func TestImportFromFileAcceptsQuotedPath(t *testing.T) {
	importSandbox(t)
	p := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(p, []byte(`[{"key":"ctrl+alt+o","command":"workbench.action.quickOpen"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := importFileSubmit(t, importFileModel(t), "  '"+p+"'  ")
	if !m.importOpen {
		t.Fatalf("panel not open: %s", importToast(m))
	}
}

func TestImportFromFileReaderChoice(t *testing.T) {
	importSandbox(t)
	dir := t.TempDir()
	for name, want := range map[string]string{
		"settings.json": "Import from settings.json (Zed)",
		"config.toml":   "Import from config.toml (Helix)",
		"Personal.xml":  "Import from Personal.xml (JetBrains)",
	} {
		body := map[string]string{
			"settings.json": `{"tab_size": 2}`,
			"config.toml":   "[editor]\nline-number = \"relative\"\n",
			"Personal.xml":  `<keymap name="P"><action id="GotoFile"><keyboard-shortcut first-keystroke="control alt P"/></action></keymap>`,
		}[name]
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		m := importFileSubmit(t, importFileModel(t), p)
		if !m.importOpen || !strings.Contains(ansi.Strip(m.View()), want) {
			t.Errorf("%s: panel open=%v\n%s", name, m.importOpen, ansi.Strip(m.View()))
		}
	}
}

func TestImportFromFileRefusalsChangeNothing(t *testing.T) {
	home, xdg := importSandbox(t)
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"unknown file": write("notes.txt", "hello"),
		"oversize":     write("settings.json", `{"a":"`+strings.Repeat("x", 1<<20+10)+`"}`),
		"directory":    dir,
		"parse error":  write("keybindings.json", `[{"key": `),
		"missing":      filepath.Join(dir, "missing.json"),
		"empty":        "   ",
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			m := importFileModel(t)
			next, cmd := m.Update(prompt.SubmitMsg{Value: p})
			m = importPlanFeed(next.(Model), cmd)
			if m.importOpen {
				t.Fatal("panel opened")
			}
			if txt := importToast(m); txt == "" {
				t.Error("no message")
			}
		})
	}
	if len(importTree(t, home)) != 0 {
		t.Errorf("files appeared in home: %v", importTree(t, home))
	}
	for name := range importTree(t, xdg) {
		switch name {
		case filepath.Join("termocode", "palette_recents.json"), filepath.Join("termocode", "errors.log"):
		default:
			t.Errorf("%s appeared in the config folder", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Error(err)
	}
}

func TestImportFromFilePromptCompletesPaths(t *testing.T) {
	importSandbox(t)
	m := importFileModel(t)
	if got := ansi.Strip(m.View()); !strings.Contains(got, "Tab") {
		t.Errorf("prompt does not mention Tab completion:\n%s", got)
	}
}
