package app

// Preferences: Undo Last Import: puts config.json and keymap.json back
// exactly as they were before the last import (byte for byte; a file the
// import created is removed) and reloads them. backup/last-import.json
// names the backup, so it works after a restart too; it is removed after
// a good undo, so a second undo has nothing to do.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// lastImport reads the undo marker and its backup. ok is false when
// there is nothing to undo.
func lastImport() (bdir string, man importManifest, ok bool, err error) {
	root, err := importBackupRoot()
	if err != nil {
		return "", man, false, err
	}
	b, err := os.ReadFile(filepath.Join(root, "last-import.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", man, false, nil
	}
	if err != nil {
		return "", man, false, err
	}
	var mk importMarker
	if err := json.Unmarshal(b, &mk); err != nil || mk.Dir == "" || filepath.Base(mk.Dir) != mk.Dir || !strings.HasPrefix(mk.Dir, "import-") {
		return "", man, false, fmt.Errorf("the undo record %s is damaged", filepath.Join(root, "last-import.json"))
	}
	bdir = filepath.Join(root, mk.Dir)
	mb, err := os.ReadFile(filepath.Join(bdir, "manifest.json"))
	if err != nil {
		return "", man, false, fmt.Errorf("the backup of the last import is missing: %w", err)
	}
	if err := json.Unmarshal(mb, &man); err != nil {
		return "", man, false, fmt.Errorf("the backup of the last import is damaged: %w", err)
	}
	return bdir, man, true, nil
}

// importFileNames lists the files of a backup ("config.json and keymap.json").
func importFileNames(man importManifest) string {
	var names []string
	for _, f := range man.Files {
		names = append(names, f.Name)
	}
	return strings.Join(names, " and ")
}

// undoLastImport asks before it puts the files back.
func (m *Model) undoLastImport() tea.Cmd {
	_, man, ok, err := lastImport()
	if err != nil {
		return m.toastErr("Cannot undo the import", err)
	}
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Nothing to undo", "No import from another editor to undo.")
		return c
	}
	when := man.Time.Local().Format("2006-01-02 15:04")
	body := fmt.Sprintf("Puts back %s as they were before the import from %s on %s.\n\n"+
		"Changes you made to these files after the import are undone too.", importFileNames(man), man.Source, when)
	m.confirm = confirm.New("Undo Last Import?", body, []confirm.Button{
		{ID: "undo", Title: "Undo Import", Style: confirm.StylePrimary},
		{ID: "cancel", Title: "Cancel"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindImportUndo
	return nil
}

// runUndoImport restores the backup and reloads keymap, theme and settings.
func (m *Model) runUndoImport() tea.Cmd {
	bdir, man, ok, err := lastImport()
	if err != nil {
		return m.toastErr("Undo failed", err)
	}
	if !ok {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return m.toastErr("Undo failed", err)
	}
	prev := loadSettings()
	if err := importRestore(dir, bdir, man); err != nil {
		recordError("undo import: " + err.Error())
		return m.toastErr("Undo failed", fmt.Errorf("%w (the backup is in %s)", err, bdir))
	}
	root := filepath.Dir(bdir)
	if err := os.Remove(filepath.Join(root, "last-import.json")); err != nil && !os.IsNotExist(err) {
		recordError("undo import: " + err.Error())
	}
	cmd := m.reloadAfterImport(prev, loadSettings())
	if m.setup != nil {
		m.setup.importedFrom = "" // the setup card's Import row offers it again
	}
	var tc tea.Cmd
	m.toast, tc = m.toast.PushDetail(toast.Info, "Import undone", importFileNames(man)+" restored")
	return tea.Batch(cmd, tc)
}
