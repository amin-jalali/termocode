package app

// Preferences: Import from Another Editor… (EM-4b): the source picker,
// the review panel (internal/importview) and Undo Last Import. Reading
// the other editor never writes anything; termocode's files change only
// after the final confirm (import_apply.go).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/importview"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// importGuidePage is the built-in guide page of the import.
const importGuidePage = "guides/import-from-another-editor.md"

// importState is the import flow's state; nil until first used.
type importState struct {
	sources []migrate.Source // the picker rows, in order
	panel   importview.Model
}

func (m *Model) importS() *importState {
	if m.imp == nil {
		m.imp = &importState{}
	}
	return m.imp
}

// importPlanMsg carries a source read off the UI goroutine.
type importPlanMsg struct {
	plan migrate.Plan
	err  error
}

func importPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "prefs-import-editor", Title: "Preferences: Import from Another Editor…"},
		{ID: "prefs-import-undo", Title: "Preferences: Undo Last Import…"},
	}
}

func (m *Model) dispatchImportPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "prefs-import-editor":
		return m.openImportPicker(), true
	case "prefs-import-undo":
		return m.undoLastImport(), true
	}
	return nil, false
}

// importGOOS is the system whose editor folders importDetect looks in
// (tests set it to check the macOS and Linux places on any machine).
var importGOOS = runtime.GOOS

// importDetect finds the other editors' config folders of this user.
func importDetect() []migrate.Source {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return migrate.Detect(home, importGOOS, os.Getenv("XDG_CONFIG_HOME"))
}

// importShowPath shows an editor folder briefly: "~/…" under the home
// folder, "$XDG_CONFIG_HOME/…" under a config root kept elsewhere.
func importShowPath(p string) string {
	if t := tildePath(p); t != p {
		return t
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" && strings.HasPrefix(p, x+string(filepath.Separator)) {
		return "$XDG_CONFIG_HOME" + p[len(x):]
	}
	return p
}

// importActionTitle is the title a user knows for a keymap action name
// ("QuickOpen" → "Go to File"): the command palette's title for the
// action. "" when the palette has none; the panel then spells the name out.
func importActionTitle(name string) string {
	act := keymap.ActionNone
	for _, a := range keymap.AllActions() {
		if keymap.ActionName(a) == name {
			act = a
			break
		}
	}
	if act == keymap.ActionNone {
		return ""
	}
	for _, it := range paletteItems() {
		if a, ok := paletteActions[it.ID]; ok && a == act {
			return strings.TrimRight(strings.TrimSuffix(strings.TrimSuffix(it.Title, "..."), "…"), " ")
		}
	}
	return ""
}

// importSourceParts says what the importer found in src ("settings, 3
// keys, theme"); "" = nothing to import.
func importSourceParts(src migrate.Source) (string, error) {
	doc, err := migrate.Read(src)
	if err != nil {
		return "", err
	}
	var parts []string
	if len(doc.Settings) > 0 {
		parts = append(parts, "settings")
	}
	if n := len(doc.Keys); n > 0 {
		parts = append(parts, importCount(n, "key"))
	}
	if doc.Theme != "" {
		parts = append(parts, "theme")
	}
	if n := len(doc.Extensions); n > 0 {
		parts = append(parts, importCount(n, "extension"))
	}
	if n := len(doc.Snippets); n > 0 {
		parts = append(parts, importCount(n, "snippet"))
	}
	return strings.Join(parts, ", "), nil
}

// importSourceHint is the picker hint: where the files are and what the
// importer found there.
func importSourceHint(src migrate.Source) string {
	parts, err := importSourceParts(src)
	where := importShowPath(src.Dir)
	if err != nil {
		return "cannot be read · " + where
	}
	if parts == "" {
		return "nothing to import · " + where
	}
	return parts + " · " + where
}

// importCount is "1 key" / "3 keys".
func importCount(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// The picker's last row and the path prompt behind it (EM-7).
const (
	importFileID    = "import-file"
	importFileTitle = "From a file…"
	importFileHint  = "a settings or keymap file" // short: the picker is only 76 cells wide at 80 columns

	promptKindImportFile promptKindEnum = 1950
)

// openImportPicker lists the editors found on this computer, then
// "From a file…" (always there, also when nothing was found).
func (m *Model) openImportPicker() tea.Cmd {
	srcs := importDetect()
	s := m.importS()
	s.sources = srcs
	m.picker = picker.NewWith(" Import from Another Editor ", func() []picker.Item {
		items := make([]picker.Item, 0, len(srcs)+1)
		for i, src := range srcs {
			items = append(items, picker.Item{ID: "import-src-" + strconv.Itoa(i), Title: src.Title, Hint: importSourceHint(src)})
		}
		hint := importFileHint
		if len(srcs) == 0 {
			hint = "Enter · " + importFileHint // the only row is the button of the empty state
		}
		return append(items, picker.Item{ID: importFileID, Title: importFileTitle, Hint: hint})
	})
	m.picker.SetWidth(96)
	if len(srcs) == 0 {
		// The empty state: a title, one plain sentence, and the row below is
		// its button.
		m.picker.SetNote([]string{"No other editor found", "Pick a settings or keymap file instead."})
	} else {
		m.picker.SetNote([]string{"Pick an editor. You see every change before anything is written."})
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindImportSource
	return m.picker.Init()
}

// openImportFilePrompt asks for the file (Tab completes the path).
func (m *Model) openImportFilePrompt() tea.Cmd {
	m.prompt = prompt.New("Import from a file", "Path of a settings or keymap file (Tab completes):", "~/")
	m.prompt.SetCompleter(prompt.CompletePath)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindImportFile
	return nil
}

// onImportFilePath reads the typed file (off the UI goroutine) and builds
// the review plan. The file is only read; a bad file is a toast and
// nothing changes.
func (m *Model) onImportFilePath(value string) tea.Cmd {
	path := strings.TrimSpace(value)
	if len(path) >= 2 && (path[0] == '\'' || path[0] == '"') && path[len(path)-1] == path[0] {
		path = strings.TrimSpace(path[1 : len(path)-1])
	}
	path = expandHome(path)
	themeID := m.themeID
	return func() tea.Msg {
		src, doc, err := migrate.ReadFile(path)
		if err != nil {
			return importPlanMsg{plan: migrate.Plan{Source: migrate.Source{Title: "the file"}}, err: err}
		}
		cur, err := importCurrent(themeID)
		if err != nil {
			return importPlanMsg{plan: migrate.Plan{Source: src}, err: err}
		}
		return importPlanMsg{plan: migrate.Build(src, doc, cur)}
	}
}

// onImportSourcePicked reads the chosen editor (off the UI goroutine)
// and builds the review plan against the user's current files.
func (m *Model) onImportSourcePicked(id string) tea.Cmd {
	if id == importFileID {
		return m.openImportFilePrompt()
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "import-src-"))
	s := m.importS()
	if err != nil || n < 0 || n >= len(s.sources) {
		return nil
	}
	src := s.sources[n]
	themeID := m.themeID
	return func() tea.Msg {
		doc, err := migrate.Read(src)
		if err != nil {
			return importPlanMsg{plan: migrate.Plan{Source: src}, err: err}
		}
		cur, err := importCurrent(themeID)
		if err != nil {
			return importPlanMsg{plan: migrate.Plan{Source: src}, err: err}
		}
		return importPlanMsg{plan: migrate.Build(src, doc, cur)}
	}
}

// importCurrent reads what the user has now: config.json, keymap.json,
// snippets.json and the theme. A keymap.json that cannot be read is an error, so an
// import can never drop the user's own bindings.
func importCurrent(themeID string) (migrate.Current, error) {
	cur := migrate.Current{Config: map[string]any{}, ThemeID: themeID}
	dir, err := configDir()
	if err != nil {
		return cur, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if len(bytes.TrimSpace(b)) == 0 {
			b = []byte("{}") // an empty file is an empty config
		}
		if err := json.Unmarshal(b, &cur.Config); err != nil {
			return cur, fmt.Errorf("config.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return cur, err
	}
	if cur.Config == nil {
		cur.Config = map[string]any{}
	}
	if t, ok := cur.Config["theme"].(string); ok && t != "" && cur.ThemeID == "" {
		cur.ThemeID = t
	}
	if cur.ThemeID == "" {
		cur.ThemeID = "vscode-dark-plus"
	}
	path := filepath.Join(dir, "keymap.json")
	if b, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(b)) == 0 {
			b = []byte("{}")
		}
		var probe map[string]any
		if err := json.Unmarshal(b, &probe); err != nil {
			return cur, fmt.Errorf("keymap.json: %w", err)
		}
		cur.KeymapRaw, cur.KeymapExists = b, true
		cur.Overrides = keymap.LoadOverrides(path)
	} else if !os.IsNotExist(err) {
		return cur, err
	}
	// A snippets.json that is not valid is not an error here: only an import
	// of snippets needs it, and that one stops without writing (migrate.Apply).
	if b, err := os.ReadFile(filepath.Join(dir, "snippets.json")); err == nil {
		if len(bytes.TrimSpace(b)) == 0 {
			b = []byte("{}")
		}
		cur.SnippetsRaw, cur.SnippetsExist = b, true
	} else if !os.IsNotExist(err) {
		return cur, err
	}
	return cur, nil
}

func (m *Model) onImportPlan(msg importPlanMsg) tea.Cmd {
	if msg.err != nil {
		return m.toastErr("Could not read "+msg.plan.Source.Title, msg.err)
	}
	s := m.importS()
	s.panel = importview.New(msg.plan, importShowPath(msg.plan.Source.Dir), importview.WithActionTitle(importActionTitle))
	s.panel.SetSize(m.w, m.h)
	m.importOpen = true
	return nil
}

// closeImport closes the review panel; nothing is written.
func (m *Model) closeImport() {
	m.importOpen = false
	if m.imp != nil {
		m.imp.panel = importview.Model{}
	}
}

func (m Model) routeToImport(msg tea.Msg) (Model, tea.Cmd) {
	s := m.importS()
	s.panel.SetSize(m.w, m.h) // a resize may have reached the confirm on top
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		s.panel.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			if id := s.panel.ButtonAt(msg.X, msg.Y); id != "" {
				m.probed("import:" + id)
			} else {
				m.probed("import")
			}
			return m, nil
		}
		var cmd tea.Cmd
		s.panel, cmd = s.panel.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	case tea.KeyMsg:
		var cmd tea.Cmd
		s.panel, cmd = s.panel.Update(msg)
		return m, cmd
	}
	return m, nil
}

// importLayer draws the review panel over base (a confirm can sit on it).
func (m Model) importLayer(base string) string {
	if !m.importOpen || m.imp == nil {
		return base
	}
	p := m.imp.panel
	p.SetSize(m.w, m.h)
	box := p.Box()
	if box == "" {
		return base
	}
	o := m.overlayOpts()
	o.At = true
	o.X, o.Y = p.Origin()
	return modalOverlay(base, box, m.w, m.h, &o)
}

// importKeysHelp is "?" in the review panel: the key card.
func (m *Model) importKeysHelp() tea.Cmd {
	return m.openKeyCard("Import keys", keyRows(
		[]string{"Space / x", "turn a row on or off (a section title: the whole section)"},
		[]string{"a / n", "turn all rows on / off"},
		[]string{"j / k", "move (PgUp, PgDn, Home and End too)"},
		[]string{"[ / ]", "previous / next section"},
		[]string{"Enter", "go on to the final check"},
		[]string{"Esc / q", "close the panel; nothing is written"},
	), help.Loc{Page: importGuidePage, Anchor: "step-by-step"}, nil)
}
