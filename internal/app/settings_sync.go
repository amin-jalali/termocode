package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// extrasState is the transient state for the Group H flows: keymap
// conflict confirm, settings export/import, and the user snippets
// manager. Kept in one struct so Model only grows by a single field.
type extrasState struct {
	// Keymap conflict: the chord + action waiting on Replace / Cancel.
	kbPendingKey    string
	kbPendingAction keymap.Action

	// Settings import: parsed bundle waiting on the confirm dialog.
	importFiles map[string][]byte

	// Snippets manager.
	snipScope      string // active scope filter; "" = all
	snipNewScope   string // scope chosen in step 1 of "New Snippet"
	snipNewTrigger string // trigger chosen in step 1 of "New Snippet"
	snipTarget     snippetRef
}

// ─── Settings bundle (pure logic) ─────────────────────────────────────
//
// Export writes one JSON file:
//
//	{
//	  "termocode_settings": 1,
//	  "exported_at": "2026-10-08T12:00:00Z",
//	  "files": { "config.json": {...}, "keymap.json": {...}, ... }
//	}
//
// Every top-level *.json in the config dir is included except state files
// (session, recents, workspaces, palette recents) and anything whose name
// looks like it holds secrets. Inside included files, object keys that
// look like secrets ("token", "api_key", "password", …) are dropped.

const settingsBundleVersion = 1

// settingsBundle is the on-disk export schema.
type settingsBundle struct {
	Version    int                        `json:"termocode_settings"`
	ExportedAt string                     `json:"exported_at,omitempty"`
	Files      map[string]json.RawMessage `json:"files"`
}

// settingsStateFiles are per-machine / per-session state, never exported.
var settingsStateFiles = map[string]bool{
	"session.json":         true,
	"recents.json":         true,
	"workspaces.json":      true,
	"palette_recents.json": true,
}

// secretWords mark a file name or JSON key as sensitive.
var secretWords = []string{
	"secret", "credential", "password", "passwd", "token",
	"apikey", "api_key", "api-key", "private_key", "authorization",
}

func looksSecret(name string) bool {
	n := strings.ToLower(name)
	for _, w := range secretWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// isExportableConfigFile reports whether a config-dir entry belongs in a
// settings export.
func isExportableConfigFile(name string) bool {
	if filepath.Base(name) != name || !strings.HasSuffix(name, ".json") {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "settings-backup-") {
		return false
	}
	if settingsStateFiles[name] {
		return false
	}
	return !looksSecret(name)
}

// redactSecrets walks a decoded JSON value and drops every object key
// that looks like a secret. Returns the cleaned value.
func redactSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if looksSecret(k) {
				delete(t, k)
				continue
			}
			t[k] = redactSecrets(child)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactSecrets(t[i])
		}
		return t
	}
	return v
}

// buildSettingsBundle reads dir and returns the export JSON plus the
// sorted list of included file names. Files that are not valid JSON are
// skipped (a half-edited file should not break the export).
func buildSettingsBundle(dir string, now time.Time) ([]byte, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	b := settingsBundle{
		Version:    settingsBundleVersion,
		ExportedAt: now.UTC().Format(time.RFC3339),
		Files:      map[string]json.RawMessage{},
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !isExportableConfigFile(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		clean, err := json.Marshal(redactSecrets(v))
		if err != nil {
			continue
		}
		b.Files[e.Name()] = clean
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(out, '\n'), names, nil
}

// parseSettingsBundle validates an export file and returns name -> pretty
// JSON body for every importable file. Unknown / unsafe names are
// dropped so a crafted bundle can't write outside the config dir or
// clobber session state.
func parseSettingsBundle(data []byte) (map[string][]byte, error) {
	var b settingsBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("not a settings file: %w", err)
	}
	if b.Version == 0 || b.Files == nil {
		return nil, errors.New("not a termocode settings export")
	}
	if b.Version > settingsBundleVersion {
		return nil, fmt.Errorf("settings export version %d is newer than this termocode", b.Version)
	}
	out := map[string][]byte{}
	for name, raw := range b.Files {
		if !isExportableConfigFile(name) {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			continue
		}
		out[name] = append(pretty, '\n')
	}
	if len(out) == 0 {
		return nil, errors.New("settings export has no files")
	}
	return out, nil
}

// writeSettingsFiles writes every file into dir, creating it if needed.
func writeSettingsFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, body := range files {
		if !isExportableConfigFile(name) {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// sortedKeys returns the keys of m in order.
func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// expandHome turns "~/x" into "$HOME/x". Other paths pass through.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

const defaultSettingsExportPath = "~/termocode-settings.json"

// ─── Settings export / import (UI) ────────────────────────────────────

func (m *Model) openSettingsExportPrompt() tea.Cmd {
	m.prompt = prompt.New("Export Settings", "Write settings to file:", defaultSettingsExportPath)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindSettingsExport
	return nil
}

func (m *Model) openSettingsImportPrompt() tea.Cmd {
	m.prompt = prompt.New("Import Settings", "Read settings from file:", defaultSettingsExportPath)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindSettingsImport
	return nil
}

// handleExtrasPromptSubmit routes the Group H prompt kinds.
func (m *Model) handleExtrasPromptSubmit(value string) tea.Cmd {
	switch m.promptKind {
	case promptKindSettingsExport:
		return m.exportSettings(value)
	case promptKindSettingsImport:
		return m.prepareSettingsImport(value)
	case promptKindSnippetNew:
		return m.onSnippetNewSubmit(value)
	case promptKindSnippetBody:
		return m.onSnippetBodySubmit(value)
	}
	return nil
}

func (m *Model) toastErr(title string, err error) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, title, err.Error())
	return c
}

func (m *Model) exportSettings(target string) tea.Cmd {
	target = expandHome(target)
	if target == "" {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return m.toastErr("Export failed", err)
	}
	data, names, err := buildSettingsBundle(dir, time.Now())
	if err != nil {
		return m.toastErr("Export failed", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return m.toastErr("Export failed", err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return m.toastErr("Export failed", err)
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info,
		fmt.Sprintf("Exported %d settings files", len(names)), target)
	return c
}

func (m *Model) prepareSettingsImport(source string) tea.Cmd {
	source = expandHome(source)
	if source == "" {
		return nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	files, err := parseSettingsBundle(data)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	m.extras.importFiles = files
	lines := []string{"These files in ~/.config/termocode will be replaced:", ""}
	for _, n := range sortedKeys(files) {
		lines = append(lines, "  "+n)
	}
	lines = append(lines, "", "A backup of your current settings is saved first.")
	m.confirm = confirm.New("Import Settings?", strings.Join(lines, "\n"), []confirm.Button{
		{ID: "import-reload", Title: "Import & Reload", Style: confirm.StylePrimary},
		{ID: "import", Title: "Import"},
		{ID: "cancel", Title: "Cancel"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSettingsImport
	return nil
}

// onSettingsImportConfirm writes the pending bundle. "import" applies
// what can be applied live (keymap, theme, snippets); "import-reload"
// also restarts termocode so every setting takes effect.
func (m *Model) onSettingsImportConfirm(id string) tea.Cmd {
	files := m.extras.importFiles
	m.extras.importFiles = nil
	if id == "cancel" || len(files) == 0 {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	// Backup first so a bad import is one re-import away from undo.
	if data, names, err := buildSettingsBundle(dir, time.Now()); err == nil && len(names) > 0 {
		backup := filepath.Join(dir, "settings-backup-"+time.Now().Format("20060102-150405")+".json")
		_ = os.WriteFile(backup, data, 0o600)
	}
	if err := writeSettingsFiles(dir, files); err != nil {
		return m.toastErr("Import failed", err)
	}
	if id == "import-reload" {
		return m.reloadWindow()
	}
	m.reloadImportedSettings()
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info,
		fmt.Sprintf("Imported %d settings files", len(files)),
		"Keymap, theme and snippets applied. Reload Window for the rest.")
	return c
}

// reloadImportedSettings re-applies the settings that can change live.
func (m *Model) reloadImportedSettings() {
	m.keys = keymap.Default().MergeInto(keymap.LoadOverrides(""))
	if id := loadSettings().Theme; id != "" {
		if _, ok := theme.LookupTheme(id); ok {
			m.applyTheme(id)
		}
	}
	m.reloadUserSnippets()
}
