package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// saveFailText turns the error of a nvim :write into short words for the
// user. nvim says "nvim:nvim_command exception: Vim(write):E45: 'readonly'
// option is set (add ! to override)", which tells a user nothing about
// what to do; the text is still in the editor, and the message says so.
func saveFailText(path string, err error) string {
	msg := err.Error()
	if strings.Contains(msg, "termocode: ") {
		return saveErrText(err) // our own sentence (a remote buffer save)
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || path == "" {
		name = "The file"
	}
	const kept = " Your changes are still in the editor."
	switch {
	case strings.Contains(msg, "E45:") || strings.Contains(msg, "E505:"):
		return name + " is read-only." + kept
	case strings.Contains(msg, "E514:"):
		return "The disk is full: " + name + " was not saved." + kept
	case strings.Contains(msg, "E212:") || strings.Contains(msg, "permission denied"):
		if dir := filepath.Dir(path); path != "" && !dirWritable(dir) {
			return "No permission to write in folder " + filepath.Base(dir) + "." + kept
		}
		return "No permission to write " + name + "." + kept
	}
	// Anything else: nvim's own words without the RPC prefix.
	if i := strings.Index(msg, "Vim(write):"); i >= 0 {
		msg = msg[i+len("Vim(write):"):]
	} else if i := strings.Index(msg, "exception: "); i >= 0 {
		msg = msg[i+len("exception: "):]
	}
	return strings.TrimSpace(msg) + "." + kept
}

// dirWritable reports whether a new file can be made in dir.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".termocode-w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// saveAll writes every modified file and tells the user how it went: a
// file that could not be written (read-only, no permission, disk full)
// is still modified afterwards, and the toast names it instead of
// claiming that everything was saved.
func (m *Model) saveAll() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if m.askSaveConflict(0, saveEveryFile) {
		return nil
	}
	_ = m.nvim.Command("silent! wall")
	var cmd tea.Cmd
	if left := m.unsavedFiles(); len(left) > 0 {
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Save failed", saveAllFailText(left))
		return cmd
	}
	m.toast, cmd = m.toast.Push(toast.Info, "Saved all open files")
	return cmd
}

// saveAllFailText names the files Save All could not write.
func saveAllFailText(paths []string) string {
	const kept = " Your changes are still in the editor."
	if len(paths) == 1 {
		return "Could not save " + filepath.Base(paths[0]) + "." + kept
	}
	names := make([]string, 0, 3)
	for i, p := range paths {
		if i == 3 {
			names = append(names, "…")
			break
		}
		names = append(names, filepath.Base(p))
	}
	return fmt.Sprintf("Could not save %d files: %s.%s", len(paths), strings.Join(names, ", "), kept)
}

// fileOpFailText says in short words why an explorer file operation
// (rename, delete, new file or folder) failed, instead of Go's
// "rename /long/path/a -> /long/path/b: permission denied".
func fileOpFailText(err error) string {
	var p string
	var le *os.LinkError
	var pe *os.PathError
	switch {
	case errors.As(err, &le):
		p = le.Old
	case errors.As(err, &pe):
		p = pe.Path
	}
	switch {
	case errors.Is(err, fs.ErrPermission) && p != "":
		return "No permission to change folder " + filepath.Base(filepath.Dir(p)) + "."
	case errors.Is(err, syscall.ENOSPC):
		return "The disk is full."
	case errors.Is(err, fs.ErrNotExist) && p != "":
		return filepath.Base(p) + " no longer exists."
	}
	return err.Error()
}
