package app

// Import from Another Editor: the final confirm, the backup, the write
// and the live reload. Only config.json, keymap.json and snippets.json in
// the termocode config dir are ever written; the other editor's files are only read.
// Order: back up the files that will change → write each one atomically
// (temp file + rename) → write the undo marker. Any error restores every
// file from the backup, so the files are as they were before.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/importview"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/migrate"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

const (
	confirmKindImportApply confirmKindEnum = 1900 + iota
	confirmKindImportUndo
)

// importFiles are the only files an import may write (in this order).
var importFiles = []string{"config.json", "keymap.json", "snippets.json"}

// importManifest describes one backup folder.
type importManifest struct {
	Source string             `json:"source"` // the other editor's name
	Time   time.Time          `json:"time"`
	Files  []importBackupFile `json:"files"`
}

type importBackupFile struct {
	Name    string      `json:"name"`
	Existed bool        `json:"existed"` // false: undo removes the file
	Mode    fs.FileMode `json:"mode,omitempty"`
}

// importMarker is backup/last-import.json: the backup Undo restores.
type importMarker struct {
	Dir string `json:"dir"` // a folder name inside backup/
}

// importBackupRoot is <config dir>/backup.
func importBackupRoot() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "backup"), nil
}

// importAtomicWrite writes data to a temp file next to path, then renames
// it over path, so a crash never leaves half a file.
func importAtomicWrite(path string, data []byte, perm fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// importWriteFile is the writer (tests swap it to fail half way).
var importWriteFile = importAtomicWrite

// importNewFiles renders the selected rows as the new file contents.
// Only files that change are returned.
func importNewFiles(plan migrate.Plan, c importview.Counts) (map[string][]byte, error) {
	out, err := migrate.Apply(plan)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	if c.Settings > 0 || c.Theme != "" {
		cfg := map[string]any{}
		if len(out.Config) > 0 {
			if err := json.Unmarshal(out.Config, &cfg); err != nil {
				return nil, err
			}
		} else {
			for k, v := range plan.Current.Config {
				cfg[k] = v
			}
		}
		if c.Theme != "" {
			cfg["theme"] = c.Theme
		}
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return nil, err
		}
		files["config.json"] = append(b, '\n')
	}
	if c.Keys > 0 {
		if len(out.Keymap) == 0 {
			return nil, errors.New("no key bindings to write")
		}
		files["keymap.json"] = append(out.Keymap, '\n')
	}
	if c.Snippets > 0 {
		if len(out.Snippets) == 0 {
			return nil, errors.New("no snippets to write")
		}
		files["snippets.json"] = out.Snippets
	}
	return files, nil
}

// importWrite backs up the files in dir that files will change, writes
// them, and records the backup for Undo. On any error every file is put
// back as it was and the error is returned.
func importWrite(dir string, files map[string][]byte, source string, now time.Time) (string, error) {
	var names []string
	for _, n := range importFiles {
		if _, ok := files[n]; ok {
			names = append(names, n)
		}
	}
	if len(names) != len(files) {
		return "", errors.New("refusing to write a file outside config.json, keymap.json and snippets.json")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	root := filepath.Join(dir, "backup")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	stamp := "import-" + now.Format("20060102-150405")
	bdir := filepath.Join(root, stamp)
	for i := 2; ; i++ {
		err := os.Mkdir(bdir, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) || i > 99 {
			return "", err
		}
		bdir = filepath.Join(root, fmt.Sprintf("%s-%d", stamp, i))
	}
	man := importManifest{Source: source, Time: now}
	for _, n := range names {
		f := importBackupFile{Name: n}
		b, err := os.ReadFile(filepath.Join(dir, n))
		switch {
		case err == nil:
			st, serr := os.Stat(filepath.Join(dir, n))
			if serr != nil {
				return "", serr
			}
			f.Existed, f.Mode = true, st.Mode().Perm()
			if err := os.WriteFile(filepath.Join(bdir, n), b, 0o600); err != nil {
				return "", fmt.Errorf("backup %s: %w", n, err)
			}
		case !os.IsNotExist(err):
			return "", fmt.Errorf("backup %s: %w", n, err)
		}
		man.Files = append(man.Files, f)
	}
	mb, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(bdir, "manifest.json"), append(mb, '\n'), 0o600); err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		if rerr := importRestore(dir, bdir, man); rerr != nil {
			return "", fmt.Errorf("%w; restoring the backup also failed: %v (the backup is in %s)", err, rerr, bdir)
		}
		return "", fmt.Errorf("%w; your files are as they were", err)
	}
	for _, f := range man.Files {
		perm := fs.FileMode(0o644)
		if f.Existed {
			perm = f.Mode
		}
		if err := importWriteFile(filepath.Join(dir, f.Name), files[f.Name], perm); err != nil {
			return fail(fmt.Errorf("write %s: %w", f.Name, err))
		}
	}
	mk, _ := json.Marshal(importMarker{Dir: filepath.Base(bdir)})
	if err := importAtomicWrite(filepath.Join(root, "last-import.json"), append(mk, '\n'), 0o600); err != nil {
		return fail(fmt.Errorf("record the import for Undo: %w", err))
	}
	return bdir, nil
}

// importRestore puts every file of a backup back, byte for byte; a file
// that did not exist before is removed.
func importRestore(dir, bdir string, man importManifest) error {
	var errs []error
	for _, f := range man.Files {
		if filepath.Base(f.Name) != f.Name || !importAllowed(f.Name) {
			errs = append(errs, fmt.Errorf("%q is not a file an import writes", f.Name))
			continue
		}
		path := filepath.Join(dir, f.Name)
		if !f.Existed {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(bdir, f.Name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := importAtomicWrite(path, b, mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func importAllowed(name string) bool {
	for _, n := range importFiles {
		if n == name {
			return true
		}
	}
	return false
}

// confirmImport opens the final confirm with what will be written.
func (m *Model) confirmImport() tea.Cmd {
	if !m.importOpen || m.imp == nil {
		return nil
	}
	c := m.imp.panel.Counts()
	if c.Total() == 0 {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, "Nothing selected",
			"Turn rows on with Space, or press a to select all.")
		return cmd
	}
	// Plain words first; the file names only in the last, quiet line.
	var what, files, items []string
	if c.Settings > 0 {
		what = append(what, "settings")
		files = append(files, "config.json")
		items = append(items, "  "+importCount(c.Settings, "setting"))
	}
	if c.Keys > 0 {
		what = append(what, "key bindings")
		files = append(files, "keymap.json")
		items = append(items, "  "+importCount(c.Keys, "key"))
	}
	if c.Theme != "" {
		what = append(what, "theme")
		files = appendNew(files, "config.json")
		name := c.Theme
		if t, ok := theme.LookupTheme(c.Theme); ok && t.Name != "" {
			name = t.Name
		}
		items = append(items, "  Theme: "+name)
	}
	if c.Snippets > 0 {
		what = append(what, "snippets")
		files = append(files, "snippets.json")
		items = append(items, "  "+importCount(c.Snippets, "snippet"))
	}
	lines := append([]string{"This changes your " + importList(what) + ".", ""}, items...)
	lines = append(lines, "")
	if len(c.Tools) > 0 {
		lines = append(lines, "Suggested tools (nothing is installed): "+strings.Join(c.Tools, ", "), "")
	}
	lines = append(lines, "A backup is saved first.", "To go back: Preferences: Undo Last Import…", "",
		"Files: "+strings.Join(files, ", "))
	m.confirm = confirm.New("Import from "+m.imp.panel.Title()+"?", strings.Join(lines, "\n"), []confirm.Button{
		{ID: "import", Title: fmt.Sprintf("Import %d %s", c.Total(), map[bool]string{true: "change", false: "changes"}[c.Total() == 1]), Style: confirm.StylePrimary},
		{ID: "cancel", Title: "Cancel"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindImportApply
	return nil
}

// importList is "a, b and c".
func importList(items []string) string {
	if n := len(items); n > 1 {
		return strings.Join(items[:n-1], ", ") + " and " + items[n-1]
	}
	return strings.Join(items, "")
}

// appendNew appends s unless it is already in list.
func appendNew(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// handleImportConfirm runs the import confirm buttons.
func (m *Model) handleImportConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindImportApply:
		if id == "import" {
			return m.applyImport(), true
		}
		return nil, true // Cancel: the panel stays open
	case confirmKindImportUndo:
		if id == "undo" {
			return m.runUndoImport(), true
		}
		return nil, true
	}
	return nil, false
}

// applyImport writes the selection, reloads it live and closes the panel.
// On an error nothing is changed and the panel stays open.
func (m *Model) applyImport() tea.Cmd {
	if !m.importOpen || m.imp == nil {
		return nil
	}
	p := m.imp.panel
	c := p.Counts()
	files, err := importNewFiles(p.Plan(), c)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	dir, err := configDir()
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	prev := loadSettings()
	if _, err := importWrite(dir, files, p.Title(), clock.Now()); err != nil {
		recordError("import from " + p.Title() + ": " + err.Error())
		return m.toastErr("Import failed", err)
	}
	cmd := m.reloadAfterImport(prev, loadSettings())
	if _, ok := files["snippets.json"]; ok {
		m.reloadUserSnippets()
	}
	m.closeImport()
	if m.setup != nil {
		m.setup.importedFrom = p.Title() // the setup card's Import row shows it done
	}
	var tc tea.Cmd
	// A click on the toast opens the Undo card; the hint comes first so a
	// narrow toast never cuts it (the summary may end in "…").
	m.toast, tc = m.toast.PushClick(toast.Info, "Imported from "+p.Title(),
		theme.ASCIIText("Click to undo · "+c.Summary()), importUndoClickMsg{})
	cmds := []tea.Cmd{cmd, tc}
	if len(c.Tools) > 0 {
		var t2 tea.Cmd
		m.toast, t2 = m.toast.PushDetail(toast.Info, "Suggested tools: "+strings.Join(c.Tools, ", "),
			"Nothing was installed. Add them with LSP: Manage Language Servers…")
		cmds = append(cmds, t2)
	}
	return tea.Batch(cmds...)
}

// reloadAfterImport applies the new keymap, theme and settings without a
// restart (the same paths the Settings UI and the theme picker use). The
// files are already written, so nothing here saves.
func (m *Model) reloadAfterImport(prev, next settingsConfig) tea.Cmd {
	quiet := m.toast
	defer func() { m.toast = quiet }()
	m.keys = keymap.Default().MergeInto(keymap.LoadOverrides(""))
	if prev.Theme != next.Theme {
		id := next.Theme
		if id == "" {
			id = "vscode-dark-plus"
		}
		if _, ok := theme.LookupTheme(id); ok && id != m.themeID {
			m.applyThemeLive(id)
		}
	}
	flipAuto, flipHidden := importToggles(prev, next, m.autoSaveOn, m.explorer.HiddenShown())
	if flipAuto {
		_ = m.toggleAutoSave()
	}
	if flipHidden {
		_ = m.toggleHiddenFiles()
	}
	on := func(b *bool) bool { return b != nil && *b }
	if on(prev.WordWrap) != on(next.WordWrap) {
		m.setWrapAll(on(next.WordWrap))
	}
	if next.TabSize != nil && *next.TabSize > 0 && (prev.TabSize == nil || *prev.TabSize != *next.TabSize) && m.nvim != nil {
		_ = m.nvim.Command(fmt.Sprintf("setglobal tabstop=%d shiftwidth=%d", *next.TabSize, *next.TabSize))
		_ = m.nvim.Command(fmt.Sprintf("set tabstop=%d shiftwidth=%d", *next.TabSize, *next.TabSize))
		return m.fetchStateCmd()
	}
	return nil
}

// importToggles says which on/off states must flip after the files
// changed: only a setting whose file value changed is followed, so a
// toggle the user made in this session (and the import did not touch)
// stays as it is.
func importToggles(prev, next settingsConfig, autoSave, hidden bool) (flipAuto, flipHidden bool) {
	on := func(b *bool) bool { return b != nil && *b }
	flipAuto = on(prev.AutoSave) != on(next.AutoSave) && on(next.AutoSave) != autoSave
	flipHidden = on(prev.ShowHidden) != on(next.ShowHidden) && on(next.ShowHidden) != hidden
	return flipAuto, flipHidden
}
