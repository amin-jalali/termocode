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

	"github.com/amin-jalali/termocode/internal/importview"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

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
		{ID: "prefs-import-editor", Title: "Preferences: Import from Another Editor..."},
		{ID: "prefs-import-undo", Title: "Preferences: Undo Last Import"},
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

// openImportPicker lists the editors found on this computer.
func (m *Model) openImportPicker() tea.Cmd {
	srcs := importDetect()
	if len(srcs) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "No other editor found",
			"No settings from another editor were found on this computer. Nothing was changed.")
		return c
	}
	s := m.importS()
	s.sources = srcs
	m.picker = picker.NewWith(" Import from Another Editor ", func() []picker.Item {
		items := make([]picker.Item, 0, len(srcs))
		for i, src := range srcs {
			items = append(items, picker.Item{ID: "import-src-" + strconv.Itoa(i), Title: src.Title, Hint: importSourceHint(src)})
		}
		return items
	})
	m.picker.SetWidth(96)
	m.picker.SetNote([]string{"Pick an editor. You see every change before anything is written."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindImportSource
	return m.picker.Init()
}

// onImportSourcePicked reads the chosen editor (off the UI goroutine)
// and builds the review plan against the user's current files.
func (m *Model) onImportSourcePicked(id string) tea.Cmd {
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

// importCurrent reads what the user has now: config.json, keymap.json
// and the theme. A keymap.json that cannot be read is an error, so an
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
	return cur, nil
}

func (m *Model) onImportPlan(msg importPlanMsg) tea.Cmd {
	if msg.err != nil {
		return m.toastErr("Could not read "+msg.plan.Source.Title, msg.err)
	}
	s := m.importS()
	s.panel = importview.New(msg.plan, importShowPath(msg.plan.Source.Dir))
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
