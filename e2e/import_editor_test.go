//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// EM-7: Import from Another Editor in the real binary. The fixtures live in
// the e2e HOME only; nothing outside it is read or written.

const (
	e2eVSCodeSettings    = "// my VS Code\n{\n  \"editor.tabSize\": 2,\n  \"editor.wordWrap\": \"on\",\n}\n"
	e2eVSCodeKeybindings = "[\n  { \"key\": \"ctrl+alt+o\", \"command\": \"workbench.action.quickOpen\" },\n]\n"
)

// vscodeUserDir is where the importer looks for VS Code on this system.
func vscodeUserDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Code", "User")
	}
	return filepath.Join(home, ".config", "Code", "User")
}

func writeVSCodeFixture(t *testing.T, home string) string {
	t.Helper()
	dir := vscodeUserDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"settings.json": e2eVSCodeSettings, "keybindings.json": e2eVSCodeKeybindings} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// treeHash is one hash over every file below dir (names and bytes).
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, _ := os.ReadFile(n)
		h.Write([]byte(n))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fileOrNone reads a file; a missing one is "<none>".
func fileOrNone(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "<none>"
	}
	return string(b)
}

func bootImport(t *testing.T, home, ws string) *Term {
	t.Helper()
	tm := Start(t, StartOpts{Cols: 140, Rows: 44, Home: home, Workspace: ws, Args: []string{"--no-session"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// importVSCode runs palette → picker → panel → confirm for the VS Code fixture.
func importVSCode(tm *Term) {
	tm.t.Helper()
	tm.Palette("Preferences: Import from Another Editor")
	tm.WaitText("Import from Another Editor", uiTimeout)
	tm.WaitText("VS Code", uiTimeout)
	tm.WaitText("From a file", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // VS Code is the first row
	tm.WaitText("editor.tabSize = 2", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // Apply...
	tm.WaitText("A backup is saved first", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter) // the primary button: Import N changes
	tm.WaitText("Imported", uiTimeout)
	tm.settle()
}

func undoImport(tm *Term) {
	tm.t.Helper()
	tm.Palette("Preferences: Undo Last Import")
	tm.WaitText("Undo Last Import?", uiTimeout)
	tm.settle()
	tm.Send("\t") // Cancel has the focus; Tab moves to Undo Import
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Import undone", uiTimeout)
	tm.settle()
}

func TestImportVSCodeApplyThenUndo(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	userDir := writeVSCodeFixture(t, home)
	other := treeHash(t, userDir)
	cfgDir := filepath.Join(home, ".config", "termocode")
	tm := bootImport(t, home, ws)
	cfgBefore, keyBefore := fileOrNone(filepath.Join(cfgDir, "config.json")), fileOrNone(filepath.Join(cfgDir, "keymap.json"))

	importVSCode(tm)

	var cfg map[string]any
	if err := json.Unmarshal([]byte(fileOrNone(filepath.Join(cfgDir, "config.json"))), &cfg); err != nil {
		t.Fatalf("config.json after import: %v", err)
	}
	if cfg["tab_size"] != float64(2) {
		t.Errorf("config.json tab_size = %v, want 2 as previewed: %v", cfg["tab_size"], cfg)
	}
	keyAfter := fileOrNone(filepath.Join(cfgDir, "keymap.json"))
	if keyAfter == keyBefore || !strings.Contains(keyAfter, "ctrl+alt+o") {
		t.Errorf("keymap.json not changed as previewed: %q", keyAfter)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "backup", "last-import.json")); err != nil {
		t.Errorf("no backup marker: %v", err)
	}
	if h := treeHash(t, userDir); h != other {
		t.Error("the VS Code files changed")
	}

	undoImport(tm)
	if got := fileOrNone(filepath.Join(cfgDir, "config.json")); got != cfgBefore {
		t.Errorf("config.json not restored:\n%q\nwant\n%q", got, cfgBefore)
	}
	if got := fileOrNone(filepath.Join(cfgDir, "keymap.json")); got != keyBefore {
		t.Errorf("keymap.json not restored:\n%q\nwant\n%q", got, keyBefore)
	}
	if h := treeHash(t, userDir); h != other {
		t.Error("the VS Code files changed after undo")
	}
	tm.Quit()
}

func TestImportUndoSurvivesARestart(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	userDir := writeVSCodeFixture(t, home)
	other := treeHash(t, userDir)
	cfgDir := filepath.Join(home, ".config", "termocode")
	tm := bootImport(t, home, ws)
	cfgBefore, keyBefore := fileOrNone(filepath.Join(cfgDir, "config.json")), fileOrNone(filepath.Join(cfgDir, "keymap.json"))
	importVSCode(tm)
	changed := fileOrNone(filepath.Join(cfgDir, "keymap.json"))
	if changed == keyBefore {
		t.Fatal("the import changed nothing")
	}
	tm.Quit()

	tm2 := bootImport(t, home, ws)
	undoImport(tm2)
	if got := fileOrNone(filepath.Join(cfgDir, "keymap.json")); got != keyBefore {
		t.Errorf("keymap.json not restored after restart:\n%q\nwant\n%q", got, keyBefore)
	}
	if strings.Contains(fileOrNone(filepath.Join(cfgDir, "config.json")), "tab_size") && !strings.Contains(cfgBefore, "tab_size") {
		t.Errorf("config.json still has the imported key: %s", fileOrNone(filepath.Join(cfgDir, "config.json")))
	}
	if h := treeHash(t, userDir); h != other {
		t.Error("the VS Code files changed")
	}
	tm2.Quit()
}

func TestImportFromAFileCancelWritesNothing(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "Preferences.sublime-settings")
	if err := os.WriteFile(file, []byte("{\n  \"tab_size\": 4,\n  \"word_wrap\": true\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileHash := treeHash(t, dir)
	cfgDir := filepath.Join(home, ".config", "termocode")
	tm := bootImport(t, home, ws)
	cfgBefore, keyBefore := fileOrNone(filepath.Join(cfgDir, "config.json")), fileOrNone(filepath.Join(cfgDir, "keymap.json"))

	tm.Palette("Preferences: Import from Another Editor")
	tm.WaitText("No other editor found", uiTimeout)
	tm.WaitText("From a file", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Import from a file", uiTimeout)
	tm.settle()
	tm.Send("\x7f\x7f") // the prompt starts with "~/"
	tm.Type(file)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Import from Preferences.sublime-settings", uiTimeout)
	tm.WaitText("tab_size = 4", uiTimeout)
	tm.settle()
	tm.Send(KeyEsc)
	tm.WaitGone("Import from Preferences.sublime-settings", uiTimeout)
	tm.settle()

	if got := fileOrNone(filepath.Join(cfgDir, "config.json")); got != cfgBefore {
		t.Errorf("config.json changed on cancel: %q", got)
	}
	if got := fileOrNone(filepath.Join(cfgDir, "keymap.json")); got != keyBefore {
		t.Errorf("keymap.json changed on cancel: %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "backup")); !os.IsNotExist(err) {
		t.Errorf("a backup folder exists after cancel: %v", err)
	}
	if h := treeHash(t, dir); h != fileHash {
		t.Error("the chosen file or its folder changed")
	}
	tm.Quit()
}

func TestImportFromAFileBadFileChangesNothing(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileHash := treeHash(t, dir)
	tm := bootImport(t, home, ws)
	tm.Palette("Preferences: Import from Another Editor")
	tm.WaitText("From a file", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Import from a file", uiTimeout)
	tm.settle()
	tm.Send("\x7f\x7f")
	tm.Type(file)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("not a supported editor file", uiTimeout)
	if tm.Has("Apply") {
		t.Error("a review panel opened for an unknown file")
	}
	if h := treeHash(t, dir); h != fileHash {
		t.Error("the chosen file's folder changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "termocode", "backup")); !os.IsNotExist(err) {
		t.Errorf("a backup folder exists: %v", err)
	}
	tm.Quit()
}
