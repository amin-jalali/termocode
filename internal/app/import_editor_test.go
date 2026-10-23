package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// importSandbox points HOME and XDG_CONFIG_HOME at fresh temp folders and
// copies the editor fixtures of internal/migrate/testdata named in
// editors ("vscode", "jetbrains", "sublime") to where Detect finds them.
func importSandbox(t *testing.T, editors ...string) (home, xdg string) {
	t.Helper()
	home, xdg = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	// macOS keeps these folders under ~/Library/Application Support,
	// not the XDG config folder.
	base, sublime := xdg, "sublime-text/Packages/User"
	if importGOOS == "darwin" {
		base, sublime = filepath.Join(home, "Library", "Application Support"), "Sublime Text/Packages/User"
	}
	for _, e := range editors {
		fx := map[string][2]string{
			"vscode":    {"vscode-review", "Code/User"},
			"jetbrains": {"jetbrains-linux", "JetBrains/GoLand2025.1"},
			"sublime":   {"sublime-linux", sublime},
		}[e]
		copyTree(t, filepath.Join("..", "migrate", "testdata", fx[0]), filepath.Join(base, fx[1]))
	}
	return home, xdg
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// importUnicodeIcons puts the Unicode icon mode on for one test: the tests
// that pin the Unicode form of the texts (→ and ·) say so (IM-12). The
// default of the test binary is ASCII, where they read "->" and "-".
func importUnicodeIcons(t *testing.T) {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

func importModel(w, h int) Model {
	return Model{w: w, h: h, toast: toast.New(), themeID: "vscode-dark-plus", editor: editor.New(nil)}
}

// importToast is every visible toast as plain text.
func importToast(m Model) string {
	return ansi.Strip(strings.Join(m.toast.View(120, 40), "\n"))
}

// importFeed runs one command (not a batch, not a timer) and hands its message
// to m.Update, the way the Bubble Tea loop does.
func importFeed(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	next, more := m.Update(cmd())
	return next.(Model), more
}

// pickerItems loads the open picker and returns its rows.
func pickerItems(t *testing.T, m *Model) []picker.Item {
	t.Helper()
	if !m.pickerOpen {
		t.Fatal("picker is not open")
	}
	if init := m.picker.Init(); init != nil {
		m.picker, _ = m.picker.Update(init())
	}
	var out []picker.Item
	for {
		it, ok := m.picker.Current()
		if !ok {
			break
		}
		if len(out) > 0 && (it.ID == out[0].ID || it.ID == out[len(out)-1].ID) || len(out) > 50 {
			break
		}
		out = append(out, it)
		m.picker, _ = m.picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	return out
}

func TestImportPaletteEntriesNeverForTheAI(t *testing.T) {
	want := map[string]string{
		"prefs-import-editor": "Preferences: Import from Another Editor…",
		"prefs-import-undo":   "Preferences: Undo Last Import…",
	}
	for _, it := range paletteItems() {
		if title, ok := want[it.ID]; ok {
			if it.Title != title {
				t.Errorf("%s: title %q, want %q", it.ID, it.Title, title)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing palette entries: %v", want)
	}
	for _, id := range []string{"prefs-import-editor", "prefs-import-undo"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s: %+v, want never with a reason", id, p)
		}
	}
}

func TestImportPickerListsDetectedEditors(t *testing.T) {
	importSandbox(t, "vscode", "jetbrains", "sublime")
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	if m.pickerKind != pickerKindImportSource {
		t.Fatalf("picker kind %v", m.pickerKind)
	}
	items := pickerItems(t, &m)
	got := map[string]string{}
	for _, it := range items {
		got[it.Title] = it.Hint
	}
	for title, hint := range map[string]string{
		"VS Code":        "4 keys",
		"GoLand 2025.1":  "keys",
		"Sublime Text 4": "theme",
	} {
		h, ok := got[title]
		if !ok {
			t.Errorf("no picker row %q in %v", title, got)
			continue
		}
		if !strings.Contains(h, hint) || !strings.HasPrefix(h, "settings") {
			t.Errorf("%s: hint %q, want it to start with what was found and mention %q", title, h, hint)
		}
	}
}

func TestImportPickerWithoutEditorsSaysSo(t *testing.T) {
	importUnicodeIcons(t)
	// EM-7: no toast any more; the picker opens with only "From a file..."
	// and a note that says no editor was found.
	importSandbox(t)
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-editor")
	if !m.pickerOpen || m.importOpen {
		t.Fatal("no picker with nothing detected")
	}
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "No other editor found") {
		t.Errorf("picker note missing:\n%s", frame)
	}
	// C-5: a title, one plain sentence, and the only row is the button with its key.
	pickerItems(t, &m) // the rows load
	frame = ansi.Strip(m.View())
	i, j, k := strings.Index(frame, "No other editor found"), strings.Index(frame, "Pick a settings or keymap file instead."), strings.Index(frame, "From a file…")
	if !(i >= 0 && j > i && k > j) || !strings.Contains(frame, "Enter · a settings or keymap file") {
		t.Errorf("not title, sentence, button:\n%s", frame)
	}
}

// openImportPanel runs the palette → picker → source flow for title.
func openImportPanel(t *testing.T, m Model, title string) Model {
	t.Helper()
	m.dispatchPaletteAction("prefs-import-editor")
	for _, it := range pickerItems(t, &m) {
		if it.Title == title {
			m.pickerOpen = false
			m, _ = importFeed(t, m, m.handlePickerSelect(picker.SelectMsg{ID: it.ID, Title: it.Title}))
			if !m.importOpen {
				t.Fatalf("panel did not open for %s: %s", title, importToast(m))
			}
			return m
		}
	}
	t.Fatalf("no source %q", title)
	return m
}

func TestImportSourceOpensTheReviewPanel(t *testing.T) {
	_, xdg := importSandbox(t, "vscode", "jetbrains")
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	frame := ansi.Strip(m.View())
	for _, s := range []string{"Import from VS Code", "In termocode", "editor.tabSize = 2", "Tab size = 2", "Apply…"} {
		if !strings.Contains(frame, s) {
			t.Errorf("frame does not show %q", s)
		}
	}
	// Keys reach the panel; Esc closes it and writes nothing.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = importFeed(t, next.(Model), cmd)
	if m.importOpen {
		t.Fatal("Esc did not close the panel")
	}
	for _, f := range []string{"config.json", "keymap.json", "user_theme.json", "backup"} {
		if _, err := os.Stat(filepath.Join(xdg, "termocode", f)); !os.IsNotExist(err) {
			t.Fatalf("%s was written before any confirm: %v", f, err)
		}
	}
	// JetBrains products share one source id: the folder tells them apart.
	m = openImportPanel(t, importModel(150, 42), "GoLand 2025.1")
	if frame := ansi.Strip(m.View()); !strings.Contains(frame, "Import from GoLand 2025.1") {
		t.Errorf("JetBrains panel:\n%s", frame)
	}
}

func TestImportPanelNarrowWindows(t *testing.T) {
	importSandbox(t, "vscode")
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	for _, sz := range [][2]int{{80, 24}, {40, 12}, {20, 6}, {5, 3}, {1, 1}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = next.(Model)
		_ = m.View()
		next, _ = m.Update(tea.MouseMsg{X: sz[0] / 2, Y: sz[1] / 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		m = next.(Model)
	}
}

func TestUndoLastImportWithNothingToUndo(t *testing.T) {
	importSandbox(t)
	m := importModel(150, 42)
	m.dispatchPaletteAction("prefs-import-undo")
	if m.confirmOpen {
		t.Fatal("a confirm opened with nothing to undo")
	}
	if txt := importToast(m); !strings.Contains(txt, "Nothing to undo") {
		t.Errorf("toast %q", txt)
	}
}

func TestImportShowPath(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	for in, want := range map[string]string{
		filepath.Join(home, ".config", "zed"):    "~/.config/zed",
		filepath.Join(xdg, "Code", "User"):       "$XDG_CONFIG_HOME/Code/User",
		"/opt/elsewhere/Code/User":               "/opt/elsewhere/Code/User",
		filepath.Join(xdg+"-other", "Code/User"): filepath.Join(xdg+"-other", "Code/User"),
	} {
		if got := importShowPath(in); got != want {
			t.Errorf("importShowPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// IM-1: a key row shows the command's palette title, not the action name.
func TestImportActionTitleUsesPaletteTitles(t *testing.T) {
	for name, want := range map[string]string{
		"QuickOpen":          "Go to File",
		"FocusExplorer":      "View: Focus Explorer",
		"DebugStartContinue": "Debug: Start / Continue",
		"CommandPalette":     "", // the palette cannot list itself: the panel spells the name out
		"NoSuchAction":       "",
	} {
		if got := importActionTitle(name); got != want {
			t.Errorf("importActionTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestImportPanelShowsCommandTitles(t *testing.T) {
	importUnicodeIcons(t)
	importSandbox(t, "vscode")
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	frame := ansi.Strip(m.View())
	for _, s := range []string{"ctrl+alt+o → Go to File", "Command palette"} {
		if !strings.Contains(frame, s) {
			t.Errorf("frame does not show %q:\n%s", s, frame)
		}
	}
	for _, id := range []string{"QuickOpen", "CommandPalette", "FocusExplorer", "DebugStartContinue", "vscode-dark-plus"} {
		if strings.Contains(frame, id) {
			t.Errorf("frame still shows the id %q", id)
		}
	}
}

// IM-4: the two "import" commands say what they read, and every title
// uses the single … character (not three dots).
func TestSettingsImportTitlesAreClear(t *testing.T) {
	want := map[string]string{
		"prefs-import-editor": "Preferences: Import from Another Editor…",
		"settings-import":     "Preferences: Import termocode Settings File…",
		"settings-export":     "Preferences: Export termocode Settings File…",
	}
	for _, it := range paletteItems() {
		if title, ok := want[it.ID]; ok {
			if it.Title != title {
				t.Errorf("%s: title %q, want %q", it.ID, it.Title, title)
			}
			if strings.Contains(it.Title, "...") {
				t.Errorf("%s: %q uses ASCII dots", it.ID, it.Title)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing palette entries: %v", want)
	}
}

// The screenshot scenes (import_demo.go) open the real cards, in the
// sandbox config dir only.
func TestImportDemoConfirmOpensTheFinalCard(t *testing.T) {
	_, xdg := importSandbox(t)
	m := importModel(150, 42)
	if cmd := m.importDemo("confirm"); cmd != nil {
		cmd() // nothing to wait for; a batch of no-ops
	}
	if !m.importOpen || !m.confirmOpen || m.confirmKind != confirmKindImportApply {
		t.Fatalf("panel %v, confirm %v kind %v", m.importOpen, m.confirmOpen, m.confirmKind)
	}
	if _, err := os.Stat(filepath.Join(xdg, "termocode", "config.json")); !os.IsNotExist(err) {
		t.Errorf("the demo wrote config.json before any confirm: %v", err)
	}
	// Outside a sandbox the demo does nothing.
	t.Setenv("XDG_CONFIG_HOME", "")
	m2 := importModel(150, 42)
	if cmd := m2.importDemo("confirm"); cmd != nil || m2.importOpen {
		t.Error("the demo ran without XDG_CONFIG_HOME")
	}
}

func TestImportDemoUndoImportsThenOpensTheUndoCard(t *testing.T) {
	_, xdg := importSandbox(t)
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	m := importModel(150, 42)
	m.importDemo("undo")
	if m.importOpen || !m.confirmOpen || m.confirmKind != confirmKindImportUndo {
		t.Fatalf("panel %v, confirm %v kind %v", m.importOpen, m.confirmOpen, m.confirmKind)
	}
	if _, err := os.Stat(filepath.Join(xdg, "termocode", "config.json")); err != nil {
		t.Errorf("the demo import wrote no config.json: %v", err)
	}
	if got := ansi.Strip(m.confirm.Box()); !strings.Contains(got, "Undo Last Import?") {
		t.Errorf("card:\n%s", got)
	}
}

func TestImportDemoSnippetsHasOneClash(t *testing.T) {
	_, xdg := importSandbox(t)
	m := importModel(150, 42)
	m.importDemo("snippets")
	if !m.importOpen {
		t.Fatal("no panel")
	}
	var snippets, clashes int
	for _, r := range m.imp.panel.Plan().Rows {
		if r.Kind == migrate.RowSnippet {
			snippets++
			if r.Conflict {
				clashes++
			}
		}
	}
	if snippets != 3 || clashes != 1 {
		t.Errorf("%d snippets, %d clashes; want 3 and 1", snippets, clashes)
	}
	// Only the sandbox was written, and only samples.
	if _, err := os.Stat(filepath.Join(xdg, "termocode", "snippets.json")); err != nil {
		t.Errorf("sample user snippets: %v", err)
	}
}

// IM-12: in ASCII icon mode the import texts are ASCII: "->" for the arrow,
// "-" for the middle dot, in the review panel, the picker hints and the
// toast. The Unicode modes keep "→" and "·" (pinned by the tests above).
func TestImportTextsAreASCIIInASCIIMode(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeASCII
	t.Cleanup(func() { theme.CurrentIconMode = old })
	importSandbox(t, "vscode")
	m := openImportPanel(t, importModel(150, 42), "VS Code")
	frame := ansi.Strip(m.View())
	for _, s := range []string{"ctrl+alt+o -> Go to File"} {
		if !strings.Contains(frame, s) {
			t.Errorf("the panel does not show %q:\n%s", s, frame)
		}
	}
	if strings.ContainsAny(frame, "→·") {
		t.Errorf("a non-ASCII arrow or dot is left in the panel:\n%s", frame)
	}
	m2 := importModel(150, 42)
	m2.dispatchPaletteAction("prefs-import-editor")
	for _, it := range pickerItems(t, &m2) {
		if strings.ContainsAny(it.Hint+it.Title, "→·") {
			t.Errorf("a non-ASCII arrow or dot in the picker row %+v", it)
		}
	}
	if got := importSourceHint(m.imp.sources[0]); !strings.Contains(got, " - ") || strings.Contains(got, "·") {
		t.Errorf("source hint = %q", got)
	}
}
