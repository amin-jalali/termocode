package app

import (
	"context"
	"errors"
	"path"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// File operations of the explorer in a container workspace (plan RC
// phase 2): new file, new folder, rename / move and delete go through
// the workspace Host FS, never the local disk. The prompts and the
// confirm cards are the same as for a local folder.

// remoteOpFailed shows a failed file operation: m.err and an error toast.
func (m *Model) remoteOpFailed(title, msg string) tea.Cmd {
	m.err = msg
	return m.pushToastDetail(toast.Errr, title, msg)
}

// remoteNewFile makes the empty file dir/name in the container and
// opens it.
func (m *Model) remoteNewFile(fsys workspace.FS, dir, name string) tea.Cmd {
	ws := m.workspace()
	p := path.Join(dir, name)
	if _, err := workspace.Resolve(ws.Root, p); err != nil {
		return m.remoteOpFailed("New file failed", remoteErrText(ws.Host.Label(), err))
	}
	if _, err := fsys.Stat(context.Background(), p); err == nil {
		return m.remoteOpFailed("New file failed", name+" already exists")
	}
	warn, err := remoteFilesFor(ws).writeFile(p, nil)
	if err != nil {
		return m.remoteOpFailed("New file failed", remoteErrText(ws.Host.Label(), err))
	}
	m.err = ""
	m.explorer.Reload()
	m.ensureEditorWindowCurrent()
	if err := m.editor.Open(p); err == nil {
		m.focus = FocusEditor
	}
	var cmd tea.Cmd
	if warn != "" {
		cmd = m.pushToastDetail(toast.Warn, "Owned by root", warn)
	}
	return tea.Batch(cmd, m.refreshSearchResults())
}

// remoteNewFolder makes the folder dir/name (and missing parents) in the
// container.
func (m *Model) remoteNewFolder(fsys workspace.FS, dir, name string) tea.Cmd {
	label := m.workspace().Host.Label()
	if err := fsys.Mkdir(context.Background(), path.Join(dir, name), 0o755); err != nil {
		return m.remoteOpFailed("New folder failed", remoteErrText(label, err))
	}
	m.err = ""
	m.explorer.Reload()
	return nil
}

// remoteRename renames or moves from to the name value (a path in value
// moves it to another folder) in the container. An existing target is
// refused: it would be replaced without a word.
func (m *Model) remoteRename(fsys workspace.FS, from, value string) tea.Cmd {
	label := m.workspace().Host.Label()
	to := path.Join(path.Dir(from), value)
	if to == from {
		return nil
	}
	ctx := context.Background()
	if _, err := fsys.Stat(ctx, to); err == nil {
		return m.remoteOpFailed("Rename failed", value+" already exists")
	}
	if err := fsys.Rename(ctx, from, to); err != nil {
		return m.remoteOpFailed("Rename failed", remoteErrText(label, err))
	}
	m.err = ""
	m.followRename(from, to)
	m.explorer.Reload()
	return m.refreshSearchResults()
}

// removePath deletes a file or folder of the workspace: in the container
// for a container workspace (the error is one clear sentence), else on
// the local disk.
func (m *Model) removePath(p string) error {
	fsys, ok := m.remoteFS()
	if !ok {
		return osRemoveAll(p)
	}
	if err := fsys.Remove(context.Background(), p); err != nil {
		return errors.New(remoteErrText(m.workspace().Host.Label(), err))
	}
	return nil
}
