package app

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Three VS Code snippets; the panel lists them in this order.
const importSnippetGo = `{
  // comments are fine
  "A one": {"prefix": "one", "body": ["if err != nil {", "\treturn err", "}"], "description": "first"},
  "B two": {"prefix": "two", "body": "two(${1:x})$0"},
  "C three": {"prefix": "three", "body": "$TM_FILENAME_BASE"},
}`

// importUserSnippets is the user's own snippets.json (odd spacing on
// purpose: a restore must give back exactly these bytes).
const importOrigSnippets = "{ \"go\": { \"mine\": \"m\" },\n   \"*\":{\"todo\":[\"a\",\"b\"]} }\n"

// importVSCodeUser is the VS Code User folder of the sandbox (macOS and
// Linux put it in different places).
func importVSCodeUser(home, xdg string) string {
	if importGOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Code", "User")
	}
	return filepath.Join(xdg, "Code", "User")
}

func importWriteSnippets(t *testing.T, home, xdg string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(importVSCodeUser(home, xdg), "snippets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// importSnippetsConfirm opens the VS Code panel, turns everything off, then
// the snippets named in picks (0-based among the three) on, and presses Enter.
func importSnippetsConfirm(t *testing.T, picks ...int) Model {
	t.Helper()
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	m, _ = importKey(m, "n")
	for i := 0; i < 3; i++ {
		m, _ = importKey(m, "]") // Settings, Keys, Theme, then Snippets
	}
	at := -1
	for _, p := range picks {
		for at < p {
			m, _ = importKey(m, "down")
			at++
		}
		m, _ = importKey(m, " ")
	}
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	if !m.confirmOpen || m.confirmKind != confirmKindImportApply {
		t.Fatalf("no final confirm: %s", importToast(m))
	}
	return m
}

func TestImportSnippetsWritesExactlyTheSelectedAndUndoRestores(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			old := importGOOS
			importGOOS = goos
			t.Cleanup(func() { importGOOS = old })
			home, xdg := importSandbox(t, "vscode")
			importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
			dir := importUserFiles(t, xdg)
			snipPath := filepath.Join(dir, "snippets.json")
			if err := os.WriteFile(snipPath, []byte(importOrigSnippets), 0o644); err != nil {
				t.Fatal(err)
			}
			m := importSnippetsConfirm(t, 0, 2)
			body := ansi.Strip(m.confirm.Box())
			for _, s := range []string{"2 snippets", "snippets.json", "Undo Last Import"} {
				if !strings.Contains(body, s) {
					t.Errorf("confirm does not say %q:\n%s", s, body)
				}
			}
			if strings.Contains(body, "config.json") || strings.Contains(body, "keymap.json") {
				t.Errorf("confirm mentions files that will not change:\n%s", body)
			}
			m = importConfirm(t, m, "import")
			if !strings.Contains(importToast(m), "2 snippets") {
				t.Errorf("toast %q", importToast(m))
			}
			got := readJSON(t, snipPath)
			goSn, _ := got["go"].(map[string]any)
			if len(got) != 2 || len(goSn) != 3 || goSn["mine"] != "m" || goSn["two"] != nil {
				t.Errorf("snippets.json = %v", got)
			}
			if l, _ := goSn["one"].([]any); len(l) != 3 || l[0] != "if err != nil {" {
				t.Errorf("one = %v", goSn["one"])
			}
			if goSn["three"] != "${TM_FILENAME_BASE}" {
				t.Errorf("three = %v", goSn["three"])
			}
			// Only snippets.json changed.
			for name, orig := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
				if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != orig {
					t.Errorf("%s changed by a snippets-only import", name)
				}
			}
			// The backup is the original, byte for byte.
			dirs, _ := filepath.Glob(filepath.Join(dir, "backup", "import-*"))
			if len(dirs) != 1 {
				t.Fatalf("backups: %v", dirs)
			}
			if b, err := os.ReadFile(filepath.Join(dirs[0], "snippets.json")); err != nil || string(b) != importOrigSnippets {
				t.Errorf("backup = %q (%v)", b, err)
			}
			// Undo, after a "restart".
			m2 := importModel(150, 42)
			m2.dispatchPaletteAction("prefs-import-undo")
			if body := ansi.Strip(m2.confirm.Box()); !strings.Contains(body, "snippets.json") {
				t.Errorf("undo confirm does not say snippets.json:\n%s", body)
			}
			m2 = importConfirm(t, m2, "undo")
			if b, err := os.ReadFile(snipPath); err != nil || string(b) != importOrigSnippets {
				t.Errorf("after undo snippets.json = %q (%v)", b, err)
			}
		})
	}
}

func TestImportSnippetsUndoRemovesAFileTheImportCreated(t *testing.T) {
	home, xdg := importSandbox(t, "vscode")
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
	dir := filepath.Join(xdg, "termocode")
	m := importConfirm(t, importSnippetsConfirm(t, 1), "import")
	if !strings.Contains(importToast(m), "1 snippet") {
		t.Fatalf("toast %q", importToast(m))
	}
	got := readJSON(t, filepath.Join(dir, "snippets.json"))
	if gs, _ := got["go"].(map[string]any); len(got) != 1 || gs["two"] != "two(${1:x})$0" || len(gs) != 1 {
		t.Errorf("snippets.json = %v", got)
	}
	m2 := importModel(150, 42)
	m2.dispatchPaletteAction("prefs-import-undo")
	importConfirm(t, m2, "undo")
	if _, err := os.Stat(filepath.Join(dir, "snippets.json")); !os.IsNotExist(err) {
		t.Errorf("snippets.json still there after undo: %v", err)
	}
}

func TestImportSnippetClashIsOffUntilTurnedOn(t *testing.T) {
	home, xdg := importSandbox(t, "vscode")
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
	dir := importUserFiles(t, xdg)
	if err := os.WriteFile(filepath.Join(dir, "snippets.json"), []byte(`{"go": {"one": "my own"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := openImportPanel(t, importModel(160, 46), "VS Code")
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "my own → if err != nil {") {
		t.Errorf("the clash does not show current → new:\n%s", frame)
	}
	on := 0
	for _, r := range m.imp.panel.Plan().Rows {
		if r.Kind == migrate.RowSnippet && r.Apply {
			on++
			if r.Trigger == "one" {
				t.Error("the clashing snippet is on by default")
			}
		}
	}
	if on != 2 {
		t.Errorf("%d snippets on, want the 2 free ones", on)
	}
}

func TestImportSnippetsPickerAndSetupSummaryCountSnippets(t *testing.T) {
	home, xdg := importSandbox(t, "vscode")
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo,
		"all.code-snippets": `{"X": {"prefix": "x", "body": "x"}}`})
	srcs := importDetect()
	var vs migrate.Source
	for _, s := range srcs {
		if s.ID == "vscode" {
			vs = s
		}
	}
	parts, err := importSourceParts(vs)
	if err != nil || !strings.Contains(parts, "4 snippets") {
		t.Fatalf("parts = %q (%v)", parts, err)
	}
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	for _, it := range pickerItems(t, &m) {
		if it.Title == "VS Code" && !strings.Contains(it.Hint, "4 snippets") {
			t.Errorf("picker hint %q", it.Hint)
		}
	}
	// A folder with only snippets is still an editor to import from.
	home2, xdg2 := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home2)
	t.Setenv("XDG_CONFIG_HOME", xdg2)
	importWriteSnippets(t, home2, xdg2, map[string]string{"go.json": importSnippetGo})
	if len(importDetect()) == 0 {
		t.Error("VS Code with only snippets is not found")
	}
}

func TestImportSnippetsMalformedUserFileIsNeverOverwritten(t *testing.T) {
	home, xdg := importSandbox(t, "vscode")
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
	dir := importUserFiles(t, xdg)
	bad := "{ not json"
	snipPath := filepath.Join(dir, "snippets.json")
	if err := os.WriteFile(snipPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	m := importConfirm(t, importSnippetsConfirm(t, 0), "import")
	if txt := importToast(m); !strings.Contains(txt, "Import failed") || !strings.Contains(txt, "snippets.json") {
		t.Errorf("toast %q", txt)
	}
	if b, _ := os.ReadFile(snipPath); string(b) != bad {
		t.Errorf("snippets.json = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Error("a backup was made for an import that failed before writing")
	}
	if !m.importOpen {
		t.Error("the panel closed")
	}
}

func TestImportSnippetsFailureMidWriteRestoresAllFiles(t *testing.T) {
	home, xdg := importSandbox(t, "vscode")
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
	dir := importUserFiles(t, xdg)
	snipPath := filepath.Join(dir, "snippets.json")
	if err := os.WriteFile(snipPath, []byte(importOrigSnippets), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := importWriteFile
	t.Cleanup(func() { importWriteFile = orig })
	importWriteFile = func(path string, data []byte, perm fs.FileMode) error {
		if filepath.Base(path) == "snippets.json" {
			return errors.New("disk full")
		}
		return orig(path, data, perm)
	}
	m := openImportPanel(t, importModel(150, 42), "VS Code") // everything on
	m, cmd := importKey(m, "enter")
	m, _ = importFeed(t, m, cmd)
	m = importConfirm(t, m, "import")
	if txt := importToast(m); !strings.Contains(txt, "disk full") || !strings.Contains(txt, "as they were") {
		t.Errorf("toast %q", txt)
	}
	for name, body := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap, "snippets.json": importOrigSnippets} {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != body {
			t.Errorf("%s not restored: %q", name, b)
		}
	}
}

func TestImportSnippetsReloadLiveInNvim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	home, xdg := importSandbox(t, "vscode")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	importWriteSnippets(t, home, xdg, map[string]string{"go.json": importSnippetGo})
	importUserFiles(t, xdg)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := c.ExecLua(snippetsLua); err != nil {
		t.Fatal(err)
	}
	look := func() string {
		s, err := c.EvalLuaString(`return tostring(termocode_snippets_for('go').two)`)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	path, _ := userSnippetsPath()
	if err := c.ExecLua(userSnippetsSetupLua(path)); err != nil {
		t.Fatal(err)
	}
	if got := look(); got != "nil" {
		t.Fatalf("before the import: %q", got)
	}
	m := importSnippetsConfirm(t, 1)
	m.nvim = c
	m = importConfirm(t, m, "import")
	if got := look(); got != "two(${1:x})$0" {
		t.Errorf("after the import the snippet is %q; it was not reloaded", got)
	}
	m.dispatchPaletteAction("prefs-import-undo")
	m = importConfirm(t, m, "undo")
	if got := look(); got != "nil" {
		t.Errorf("after undo the snippet is %q; it was not reloaded", got)
	}
}
