package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
)

// covSyncModel is a model whose config dir holds a config with a
// secret, a keymap, a state file and a broken file.
func covSyncModel(t *testing.T) (Model, string) {
	t.Helper()
	root := withConfigDir(t)
	dir := filepath.Join(root, "termocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"config.json":  `{"tab_size": 2, "ai": {"api_key": "sk-secret", "model": "m"}}`,
		"keymap.json":  `{"ctrl+alt+j": "ToggleTerminal"}`,
		"recents.json": `["/a"]`,
		"broken.json":  `{"half`,
		"tokens.json":  `{"x": 1}`,
		"notes.txt":    "not json",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := covNew(t)
	m.w, m.h = 120, 40
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	return m, dir
}

func TestCovCoreSettingsExportImport(t *testing.T) {
	m, dir := covSyncModel(t)
	out := filepath.Join(t.TempDir(), "sub", "export.json")

	_ = m.dispatchPaletteAction("settings-export")
	if !m.promptOpen || m.promptKind != promptKindSettingsExport || m.prompt.Value() != defaultSettingsExportPath {
		t.Fatalf("export prompt: open=%v kind=%v value=%q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	m, _ = m.update(prompt.SubmitMsg{Value: out})
	if !strings.Contains(toastText(m), "Exported 2 settings files") {
		t.Errorf("export toast:\n%s", toastText(m))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret") || strings.Contains(string(data), "recents") || strings.Contains(string(data), "tokens") {
		t.Errorf("export holds secrets or state:\n%s", data)
	}
	if !strings.Contains(string(data), `"model": "m"`) || !strings.Contains(string(data), "ctrl+alt+j") {
		t.Errorf("export lost settings:\n%s", data)
	}

	// Change the bundle: a new keymap and a theme, then import it.
	var b settingsBundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	b.Files["keymap.json"] = json.RawMessage(`{"ctrl+alt+k": "ToggleTerminal"}`)
	b.Files["config.json"] = json.RawMessage(`{"tab_size": 4, "theme": "tokyo-night"}`)
	b.Files["../evil.json"] = json.RawMessage(`{}`)
	edited, _ := json.Marshal(b)
	if err := os.WriteFile(out, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.dispatchPaletteAction("settings-import")
	if !m.promptOpen || m.promptKind != promptKindSettingsImport {
		t.Fatalf("import prompt: open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m, _ = m.update(prompt.SubmitMsg{Value: out})
	if !m.confirmOpen || m.confirmKind != confirmKindSettingsImport {
		t.Fatalf("no import question: %v %v", m.confirmOpen, m.confirmKind)
	}
	if v := m.confirm.View(); !strings.Contains(v, "keymap.json") || strings.Contains(v, "evil") {
		t.Errorf("import question:\n%s", v)
	}
	m, _ = m.update(confirm.SelectMsg{ID: "import"})
	if !strings.Contains(toastText(m), "Imported 2 settings files") {
		t.Errorf("import toast:\n%s", toastText(m))
	}
	if m.keys.Bindings()["ctrl+alt+k"] != keymap.ActionToggleTerminal {
		t.Error("imported keymap not live")
	}
	if m.themeID != "tokyo-night" {
		t.Errorf("theme = %q, want the imported tokyo-night", m.themeID)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "settings-backup-*.json"))
	if len(backups) != 1 {
		t.Fatalf("backups = %q", backups)
	}
	old, _ := os.ReadFile(backups[0])
	if !strings.Contains(string(old), "ctrl+alt+j") {
		t.Errorf("backup does not hold the old keymap:\n%s", old)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.json")); err == nil {
		t.Error("an import wrote outside the config folder")
	}
}

func TestCovCoreSettingsImportChoices(t *testing.T) {
	m, dir := covSyncModel(t)
	bundle := filepath.Join(t.TempDir(), "b.json")
	_ = os.WriteFile(bundle, []byte(`{"termocode_settings":1,"files":{"keymap.json":{"ctrl+alt+q":"ToggleTerminal"}}}`), 0o644)

	// Cancel: nothing written.
	_ = m.prepareSettingsImport(bundle)
	m, _ = m.update(confirm.SelectMsg{ID: "cancel"})
	if b, _ := os.ReadFile(filepath.Join(dir, "keymap.json")); strings.Contains(string(b), "ctrl+alt+q") {
		t.Error("cancel imported the file")
	}
	if m.extras.importFiles != nil {
		t.Error("the parsed bundle was kept after cancel")
	}
	// A confirm with nothing pending is a no-op.
	if m.onSettingsImportConfirm("import") != nil {
		t.Error("import with no pending files did something")
	}
	// Import & Reload: files written, the reload cannot run headless.
	_ = m.prepareSettingsImport(bundle)
	m, _ = m.update(confirm.SelectMsg{ID: "import-reload"})
	if b, _ := os.ReadFile(filepath.Join(dir, "keymap.json")); !strings.Contains(string(b), "ctrl+alt+q") {
		t.Errorf("import-reload did not write: %s", b)
	}
	if !strings.Contains(toastText(m), "Reload failed") {
		t.Errorf("headless reload toast:\n%s", toastText(m))
	}
	// A folder where the file must go: the write fails.
	_ = os.Remove(filepath.Join(dir, "keymap.json"))
	_ = os.Mkdir(filepath.Join(dir, "keymap.json"), 0o755)
	_ = m.prepareSettingsImport(bundle)
	m, _ = m.update(confirm.SelectMsg{ID: "import"})
	if !strings.Contains(toastText(m), "Import failed") {
		t.Errorf("write onto a folder:\n%s", toastText(m))
	}
	// Only exportable names are written.
	if err := writeSettingsFiles(dir, map[string][]byte{"session.json": []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); err == nil {
		t.Error("a state file was written by an import")
	}
}

func TestCovCoreSettingsImportErrors(t *testing.T) {
	m, _ := covSyncModel(t)
	tmp := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(tmp, name)
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	// Empty answers do nothing.
	if m.prepareSettingsImport("  ") != nil || m.exportSettings("") != nil || m.confirmOpen {
		t.Error("an empty path did something")
	}
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(tmp, "missing.json"), "no such file"},
		{write("text.json", "hello"), "not a settings file"},
		{write("other.json", `{"files":{}}`), "not a termocode settings export"},
		{write("newer.json", `{"termocode_settings":99,"files":{"a.json":{}}}`), "version 99 is newer"},
		{write("empty.json", `{"termocode_settings":1,"files":{"session.json":{},"x.txt":{}}}`), "has no files"},
	} {
		_ = m.prepareSettingsImport(tc.path)
		if !strings.Contains(toastText(m), "Import failed") || !strings.Contains(toastText(m), tc.want) {
			t.Errorf("%s: toast misses %q:\n%s", tc.path, tc.want, toastText(m))
		}
		if m.confirmOpen {
			t.Errorf("%s: a bad file asked to import", tc.path)
		}
	}
	// "~/" is the home folder.
	home := os.Getenv("HOME")
	if got := expandHome("~/x.json"); got != filepath.Join(home, "x.json") {
		t.Errorf("expandHome = %q", got)
	}
	if got := expandHome("~"); got != home {
		t.Errorf("expandHome(~) = %q", got)
	}
}

func TestCovCoreSettingsSyncDiskErrors(t *testing.T) {
	m, dir := covSyncModel(t)
	// Export into a folder that is a file.
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o644)
	_ = m.exportSettings(filepath.Join(blocker, "x.json"))
	if !strings.Contains(toastText(m), "Export failed") {
		t.Errorf("export into a file:\n%s", toastText(m))
	}
	// Export onto a folder.
	_ = m.exportSettings(t.TempDir())
	if strings.Count(toastText(m), "Export failed") < 2 {
		t.Errorf("export onto a folder:\n%s", toastText(m))
	}
	// The config folder itself is a file: the export cannot read it and
	// an import cannot write it.
	bundle := filepath.Join(t.TempDir(), "b.json")
	_ = os.WriteFile(bundle, []byte(`{"termocode_settings":1,"files":{"keymap.json":{}}}`), 0o644)
	_ = m.prepareSettingsImport(bundle)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.exportSettings(filepath.Join(t.TempDir(), "y.json"))
	if strings.Count(toastText(m), "Export failed") < 3 {
		t.Errorf("export of an unreadable config:\n%s", toastText(m))
	}
	m, _ = m.update(confirm.SelectMsg{ID: "import"})
	if !strings.Contains(toastText(m), "Import failed") {
		t.Errorf("import into a file:\n%s", toastText(m))
	}
	// A missing config folder exports an empty bundle.
	data, names, err := buildSettingsBundle(filepath.Join(t.TempDir(), "none"), time.Now())
	if err != nil || len(names) != 0 || !strings.Contains(string(data), `"termocode_settings": 1`) {
		t.Errorf("missing dir: %s %q %v", data, names, err)
	}
}

func TestCovCoreHandleExtrasPromptOtherKind(t *testing.T) {
	m, _ := covSyncModel(t)
	m.promptKind = promptKindCommit
	if m.handleExtrasPromptSubmit("x") != nil {
		t.Error("a foreign prompt kind was handled")
	}
}
