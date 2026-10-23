package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The user's files before the import, byte for byte (odd spacing on
// purpose: a restore must give back exactly these bytes).
const (
	importOrigConfig = "{\n    \"tab_size\": 4,\n    \"custom_key\": \"keep me\",\n    \"theme\": \"vscode-dark-plus\"\n}\n"
	importOrigKeymap = "{ \"alt+z\": \"Save\" }\n"
)

// importUserFiles writes the user's own config.json / keymap.json.
func importUserFiles(t *testing.T, xdg string) string {
	t.Helper()
	dir := filepath.Join(xdg, "termocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func importKey(m Model, k string) (Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch k {
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// importReviewAndConfirm opens the VS Code panel, turns word wrap off
// (the second settings row), presses Enter and returns the model with
// the final confirm open.
func importReviewAndConfirm(t *testing.T) Model {
	t.Helper()
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	for _, k := range []string{"down", "down", " "} {
		m, _ = importKey(m, k)
	}
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	if !m.confirmOpen || m.confirmKind != confirmKindImportApply {
		t.Fatalf("no final confirm: %s", importToast(m))
	}
	return m
}

func importConfirm(t *testing.T, m Model, id string) Model {
	t.Helper()
	next, _ := m.Update(confirm.SelectMsg{ID: id})
	return next.(Model)
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return out
}

func TestImportApplyWritesExactlyTheSelectedRows(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	dir := importUserFiles(t, xdg)
	theme.ApplyTheme("vscode-dark-plus")
	darkPlusBg := theme.CurrentPaletteHex("BgEditor")
	m := importReviewAndConfirm(t)
	body := ansi.Strip(m.confirm.Box())
	for _, s := range []string{"2 settings", "4 keys", "One Dark", "config.json", "keymap.json", "backup", "Undo Last Import", "gopls"} {
		if !strings.Contains(body, s) {
			t.Errorf("confirm does not say %q:\n%s", s, body)
		}
	}
	m = importConfirm(t, m, "import")
	if m.importOpen {
		t.Error("panel still open after the import")
	}
	cfg := readJSON(t, filepath.Join(dir, "config.json"))
	want := map[string]any{"tab_size": 2.0, "auto_save": true, "custom_key": "keep me", "theme": "one-dark"}
	if len(cfg) != len(want) {
		t.Errorf("config.json = %v, want %v", cfg, want)
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("config.json %s = %v, want %v", k, cfg[k], v)
		}
	}
	km := readJSON(t, filepath.Join(dir, "keymap.json"))
	var keys []string
	for k := range km {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, " ") != "alt+z ctrl+alt+o ctrl+shift+f ctrl+shift+k ctrl+shift+p" {
		t.Errorf("keymap.json keys = %v", keys)
	}
	if km["alt+z"] != "Save" || km["ctrl+alt+o"] != "QuickOpen" {
		t.Errorf("keymap.json = %v", km)
	}
	// No other file was created or changed in the config dir.
	if _, err := os.Stat(filepath.Join(dir, "user_theme.json")); !os.IsNotExist(err) {
		t.Errorf("user_theme.json written: %v", err)
	}
	// Live reload: the keymap and the theme changed without a restart.
	if a := m.keys.Bindings()["ctrl+alt+o"]; a != keymap.ActionQuickOpen {
		t.Errorf("ctrl+alt+o is %v after the import", keymap.ActionName(a))
	}
	if m.themeID != "one-dark" || theme.CurrentPaletteHex("BgEditor") == darkPlusBg {
		t.Errorf("theme %q (editor bg %s), want one-dark live", m.themeID, theme.CurrentPaletteHex("BgEditor"))
	}
	txt := importToast(m)
	for _, s := range []string{"Imported from VS Code", "2 settings", "4 keys", "Click to undo"} {
		if !strings.Contains(txt, s) {
			t.Errorf("toast does not say %q:\n%s", s, txt)
		}
	}
}

func TestImportBackupEqualsTheOriginalFiles(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	dir := importUserFiles(t, xdg)
	m := importConfirm(t, importReviewAndConfirm(t), "import")
	_ = m
	dirs, _ := filepath.Glob(filepath.Join(dir, "backup", "import-*"))
	if len(dirs) != 1 {
		t.Fatalf("backup dirs: %v", dirs)
	}
	if st, err := os.Stat(dirs[0]); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode: %v %v", st.Mode(), err)
	}
	for name, orig := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
		b, err := os.ReadFile(filepath.Join(dirs[0], name))
		if err != nil || string(b) != orig {
			t.Errorf("backup %s = %q (%v), want %q", name, b, err, orig)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "backup", "last-import.json")); err != nil {
		t.Errorf("no last-import marker: %v", err)
	}
}

func TestImportFailureMidWriteRestoresTheFiles(t *testing.T) {
	for _, hadFiles := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing", false: "fresh"}[hadFiles], func(t *testing.T) {
			_, xdg := importSandbox(t, "vscode")
			dir := filepath.Join(xdg, "termocode")
			if hadFiles {
				importUserFiles(t, xdg)
			}
			orig := importWriteFile
			t.Cleanup(func() { importWriteFile = orig })
			importWriteFile = func(path string, data []byte, perm fs.FileMode) error {
				if filepath.Base(path) == "keymap.json" {
					return errors.New("disk full")
				}
				return orig(path, data, perm)
			}
			m := importConfirm(t, importReviewAndConfirm(t), "import")
			if txt := importToast(m); !strings.Contains(txt, "disk full") || !strings.Contains(txt, "Import failed") || !strings.Contains(txt, "as they were") {
				t.Errorf("toast %q", txt)
			}
			for name, body := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
				b, err := os.ReadFile(filepath.Join(dir, name))
				switch {
				case hadFiles && string(b) != body:
					t.Errorf("%s not restored: %q (%v)", name, b, err)
				case !hadFiles && !os.IsNotExist(err):
					t.Errorf("%s should not exist after a failed import: %q", name, b)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "backup", "last-import.json")); !os.IsNotExist(err) {
				t.Error("a failed import left an undo marker")
			}
			if m.themeID != "vscode-dark-plus" {
				t.Errorf("theme changed after a failed import: %s", m.themeID)
			}
			if !m.importOpen {
				t.Error("the panel closed after a failed import; the selection is lost")
			}
		})
	}
}

// treeHashes hashes every file below root, skipping skip.
func treeHashes(t *testing.T, root, skip string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == skip && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			b, _ := os.ReadFile(p)
			out[p] = sha256.Sum256(b)
		}
		return nil
	})
	return out
}

func TestImportNeverWritesOutsideTheConfigDir(t *testing.T) {
	home, xdg := importSandbox(t, "vscode", "jetbrains", "sublime")
	dir := importUserFiles(t, xdg)
	before := treeHashes(t, xdg, dir)
	beforeHome := treeHashes(t, home, "")
	m := importConfirm(t, importReviewAndConfirm(t), "import")
	if !strings.Contains(importToast(m), "Imported from") {
		t.Fatalf("import did not run: %s", importToast(m))
	}
	after := treeHashes(t, xdg, dir)
	if len(after) != len(before) {
		t.Errorf("files outside the termocode config dir: %d before, %d after", len(before), len(after))
	}
	for p, h := range before {
		if after[p] != h {
			t.Errorf("the other editor's file changed: %s", p)
		}
	}
	if afterHome := treeHashes(t, home, ""); len(afterHome) != len(beforeHome) {
		t.Errorf("HOME changed: %d files before, %d after", len(beforeHome), len(afterHome))
	}
}

func TestImportNothingSelectedOrCancelWritesNothing(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	dir := importUserFiles(t, xdg)
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	m, _ = importKey(m, "n")
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	if m.confirmOpen || !strings.Contains(importToast(m), "Nothing selected") {
		t.Fatalf("enter with nothing selected: confirm %v, toast %q", m.confirmOpen, importToast(m))
	}
	// Cancel on the confirm returns to the panel and writes nothing.
	m, _ = importKey(m, "a")
	m, cmd = importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	m = importConfirm(t, m, "cancel")
	if !m.importOpen {
		t.Error("Cancel closed the panel")
	}
	for name, body := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != body {
			t.Errorf("%s changed without a confirm", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Error("a backup was made without a confirm")
	}
}

func TestImportWithEmptyUserFiles(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	dir := filepath.Join(xdg, "termocode")
	_ = os.MkdirAll(dir, 0o755)
	for _, n := range []string{"config.json", "keymap.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := importConfirm(t, importReviewAndConfirm(t), "import")
	if !strings.Contains(importToast(m), "Imported from VS Code") {
		t.Fatalf("toast %q", importToast(m))
	}
	if km := readJSON(t, filepath.Join(dir, "keymap.json")); km["ctrl+alt+o"] != "QuickOpen" {
		t.Errorf("keymap.json = %v", km)
	}
}

// A reload only follows what changed in the files: a session toggle
// (auto-save, hidden files) that the import did not touch stays.
func TestImportReloadOnlyFollowsChangedSettings(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name               string
		prev, next         settingsConfig
		autoSave, hidden   bool
		flipAuto, flipHide bool
	}{
		{"untouched session toggles stay", settingsConfig{}, settingsConfig{TabSize: new(int)}, true, true, false, false},
		{"turned on by the import", settingsConfig{}, settingsConfig{AutoSave: &yes, ShowHidden: &yes}, false, false, true, true},
		{"already as the file says", settingsConfig{}, settingsConfig{AutoSave: &yes}, true, false, false, false},
		{"undo turns it off again", settingsConfig{AutoSave: &yes, ShowHidden: &yes}, settingsConfig{}, true, true, true, true},
		{"set off explicitly", settingsConfig{AutoSave: &yes}, settingsConfig{AutoSave: &no}, true, false, true, false},
	} {
		auto, hide := importToggles(tc.prev, tc.next, tc.autoSave, tc.hidden)
		if auto != tc.flipAuto || hide != tc.flipHide {
			t.Errorf("%s: flip auto-save %v, hidden %v; want %v, %v", tc.name, auto, hide, tc.flipAuto, tc.flipHide)
		}
	}
}

// The theme follows the file only when the import changed it.
func TestImportKeepsTheLiveThemeWhenTheThemeRowIsOff(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	importUserFiles(t, xdg)
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	m := importModel(150, 42)
	m.themeID = "tokyo-night" // shown now, not (yet) in config.json
	m = openImportPanel(t, m, "VS Code")
	m, _ = importKey(m, "G") // the last row is in Extensions; go to Theme
	for i := 0; i < 4; i++ {
		m, _ = importKey(m, "k")
	}
	m, _ = importKey(m, " ") // the theme row off
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	if body := ansi.Strip(m.confirm.Box()); strings.Contains(body, "theme ") {
		t.Fatalf("the theme row is still on:\n%s", body)
	}
	m = importConfirm(t, m, "import")
	if m.themeID != "tokyo-night" {
		t.Errorf("theme switched to %q by an import that did not change it", m.themeID)
	}
}

// A resize while the final confirm is open reaches the panel too: a
// click after Back hits the row drawn under the pointer.
func TestImportPanelFollowsAResizeUnderTheConfirm(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	importUserFiles(t, xdg)
	m := openImportPanel(t, importModel(200, 50), "VS Code") // the card is centred with a margin
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = importConfirm(t, next.(Model), "back")
	// Find "editor.wordWrap" on the 80x24 screen and click it.
	x, y := -1, -1
	for row, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(l, "editor.wordWrap"); i >= 0 {
			x, y = len([]rune(l[:i])), row
		}
	}
	if y < 0 {
		t.Fatal("word wrap row not on screen")
	}
	next, _ = m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = next.(Model)
	m, cmd = importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	if body := ansi.Strip(m.confirm.Box()); !strings.Contains(body, "2 settings") {
		t.Errorf("the click did not turn word wrap off:\n%s", body)
	}
}

// IM-8: the final card says "Cancel" like the other cards, tells what
// changes in plain words, and names the files only in its last line.
func TestImportConfirmCardUsesPlainWords(t *testing.T) {
	_, xdg := importSandbox(t, "vscode")
	importUserFiles(t, xdg)
	m := importReviewAndConfirm(t)
	if got := m.confirm.Buttons(); len(got) != 2 || got[0].ID != "import" || got[1].ID != "cancel" || got[1].Title != "Cancel" {
		t.Fatalf("buttons %+v", got)
	}
	body := ansi.Strip(m.confirm.Box())
	for _, s := range []string{"This changes your settings, key bindings and theme.", "2 settings", "4 keys", "Theme: One Dark", "Files: config.json, keymap.json"} {
		if !strings.Contains(body, s) {
			t.Errorf("card does not say %q:\n%s", s, body)
		}
	}
	if strings.Contains(body, "Back") || strings.Contains(body, "→ config") || strings.Contains(body, "→ keymap") {
		t.Errorf("card still has Back or file arrows:\n%s", body)
	}
	// The file names are only in the last line.
	var head []string
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "Files:") {
			break
		}
		head = append(head, l)
	}
	if h := strings.Join(head, "\n"); strings.Contains(h, "config.json") || strings.Contains(h, "keymap.json") {
		t.Errorf("a file name is above the Files line:\n%s", h)
	}
}
