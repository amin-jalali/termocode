package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// importOnce runs a whole import of the VS Code fixture and returns the
// config dir.
func importOnce(t *testing.T, withUserFiles bool) string {
	t.Helper()
	_, xdg := importSandbox(t, "vscode")
	dir := filepath.Join(xdg, "termocode")
	if withUserFiles {
		importUserFiles(t, xdg)
	}
	m := importConfirm(t, importReviewAndConfirm(t), "import")
	if !strings.Contains(importToast(m), "Imported from VS Code") {
		t.Fatalf("import failed: %s", importToast(m))
	}
	return dir
}

func TestUndoLastImportRestoresByteForByteAfterARestart(t *testing.T) {
	dir := importOnce(t, true)
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) == importOrigConfig {
		t.Fatal("the import did not change config.json")
	}
	// A new Model: termocode was restarted; only the files remain.
	m := importModel(150, 42)
	m.themeID = "one-dark"
	m.dispatchPaletteAction("prefs-import-undo")
	if !m.confirmOpen || m.confirmKind != confirmKindImportUndo {
		t.Fatalf("no undo confirm: %s", importToast(m))
	}
	body := ansi.Strip(m.confirm.Box())
	for _, s := range []string{"VS Code", "config.json", "keymap.json"} {
		if !strings.Contains(body, s) {
			t.Errorf("undo confirm does not say %q:\n%s", s, body)
		}
	}
	m = importConfirm(t, m, "undo")
	for name, orig := range map[string]string{"config.json": importOrigConfig, "keymap.json": importOrigKeymap} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != orig {
			t.Errorf("%s = %q (%v), want the original bytes %q", name, b, err, orig)
		}
	}
	// Reloaded live: the keymap and the theme are the old ones again.
	if a := m.keys.Bindings()["alt+z"]; a != keymap.ActionSave {
		t.Errorf("alt+z is %v after undo", keymap.ActionName(a))
	}
	if a, ok := m.keys.Bindings()["ctrl+alt+o"]; ok && a == keymap.ActionQuickOpen {
		t.Error("ctrl+alt+o still opens Quick Open after undo")
	}
	if m.themeID != "vscode-dark-plus" {
		t.Errorf("theme %q after undo", m.themeID)
	}
	if !strings.Contains(importToast(m), "Import undone") {
		t.Errorf("toast %q", importToast(m))
	}
	// A second undo has nothing to undo.
	m2 := importModel(150, 42)
	m2.dispatchPaletteAction("prefs-import-undo")
	if m2.confirmOpen || !strings.Contains(importToast(m2), "Nothing to undo") {
		t.Errorf("second undo: confirm %v, toast %q", m2.confirmOpen, importToast(m2))
	}
}

func TestUndoLastImportRemovesFilesTheImportCreated(t *testing.T) {
	dir := importOnce(t, false)
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-undo")
	m = importConfirm(t, m, "undo")
	for _, name := range []string{"config.json", "keymap.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after undo: %v", name, err)
		}
	}
	if !strings.Contains(importToast(m), "Import undone") {
		t.Errorf("toast %q", importToast(m))
	}
}

func TestUndoLastImportCancelKeepsTheImport(t *testing.T) {
	dir := importOnce(t, true)
	after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-undo")
	m = importConfirm(t, m, "cancel")
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) != string(after) {
		t.Error("Cancel changed config.json")
	}
	m.dispatchPaletteAction("prefs-import-undo")
	if !m.confirmOpen {
		t.Error("after Cancel the import can no longer be undone")
	}
}

// IM-3: the undo card also undoes edits made after the import, so Cancel
// is first and focused (Enter alone keeps everything) and Undo is the
// danger button.
func TestUndoCardFocusesCancelAndMarksUndoAsDanger(t *testing.T) {
	dir := importOnce(t, true)
	after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-undo")
	if !m.confirmOpen {
		t.Fatalf("no undo card: %s", importToast(m))
	}
	btns := m.confirm.Buttons()
	if len(btns) != 2 || btns[0].ID != "cancel" || btns[1].ID != "undo" || btns[1].Style != confirm.StyleDestructive || btns[0].Style == confirm.StylePrimary {
		t.Fatalf("buttons %+v", btns)
	}
	// Enter picks the focused button: Cancel.
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did nothing")
	}
	if sel, ok := cmd().(confirm.SelectMsg); !ok || sel.ID != "cancel" {
		t.Fatalf("Enter picked %#v, want cancel", cmd())
	}
	m = importConfirm(t, m, "cancel")
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) != string(after) {
		t.Error("Cancel changed config.json")
	}
}

// IM-7: the toast after an import is the way back. A click opens the Undo
// card (never undoes at once), and the hint fits a 52-column toast.
func TestImportToastIsClickableAndOpensTheUndoCard(t *testing.T) {
	importUnicodeIcons(t)
	_, xdg := importSandbox(t, "vscode")
	importUserFiles(t, xdg)
	m := importConfirm(t, importReviewAndConfirm(t), "import")
	var click tea.Msg
	var text string
	for _, c := range m.toast.Cards(52) {
		if s := ansi.Strip(strings.Join(c.Lines, "\n")); strings.Contains(s, "Imported from VS Code") {
			click, text = c.Click, s
			for _, l := range c.Lines {
				if w := lipgloss.Width(l); w > 52 {
					t.Errorf("toast row is %d wide: %q", w, l)
				}
			}
		}
	}
	if click == nil {
		t.Fatalf("the import toast is not clickable: %s", importToast(m))
	}
	if !strings.Contains(text, "Click to undo ·") {
		t.Errorf("the undo hint is cut at 52 columns:\n%s", text)
	}
	next, _ := m.Update(click)
	m = next.(Model)
	if !m.confirmOpen || m.confirmKind != confirmKindImportUndo {
		t.Fatalf("the click did not open the undo card: confirm %v kind %v", m.confirmOpen, m.confirmKind)
	}
	// The click alone undid nothing: the backup is still there.
	if _, _, ok, err := lastImport(); err != nil || !ok {
		t.Errorf("the backup is gone after the click: ok=%v err=%v", ok, err)
	}
}

// IM-13: the undo card says in plain words what it puts back ("your
// settings, key bindings and theme"), like the import card; the file names
// are only in the last line, which is drawn quiet.
func TestUndoCardUsesPlainWordsAndNamesFilesLast(t *testing.T) {
	importOnce(t, true)
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-undo")
	if !m.confirmOpen {
		t.Fatalf("no undo card: %s", importToast(m))
	}
	lines := strings.Split(ansi.Strip(m.confirm.Box()), "\n")
	var body []string
	for _, l := range lines {
		body = append(body, strings.TrimSpace(strings.Trim(l, "│")))
	}
	text := strings.Join(body, "\n")
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "Puts your settings, key bindings and theme back as they were before the import") {
		t.Errorf("not in plain words:\n%s", text)
	}
	filesAt := -1
	for i, l := range body {
		if strings.HasPrefix(l, "Files: ") {
			filesAt = i
		}
		if i < filesAt || filesAt < 0 {
			if strings.Contains(l, ".json") && !strings.HasPrefix(l, "Files: ") {
				t.Errorf("a file name before the last line: %q", l)
			}
		}
	}
	if filesAt < 0 || !strings.Contains(body[filesAt], "config.json") || !strings.Contains(body[filesAt], "keymap.json") {
		t.Errorf("no Files line with the names:\n%s", text)
	}
	for _, l := range body[filesAt+1:] {
		if strings.Contains(l, ".json") {
			t.Errorf("a file name after the Files line: %q", l)
		}
	}
}
