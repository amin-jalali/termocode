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

// Each call to the container engine runs off the UI loop (FIX-2): a
// hung engine answers only when its 10 s timeout ends, and inside Update
// that froze the whole UI. The checks that need no engine (the path is
// in the workspace) run at once; the rest runs as a command whose answer
// (an applyMsg) does the UI part in Update.

// remoteNewFile makes the empty file dir/name in the container and
// opens it.
func (m *Model) remoteNewFile(fsys workspace.FS, dir, name string) tea.Cmd {
	ws := m.workspace()
	p := path.Join(dir, name)
	if _, err := workspace.Resolve(ws.Root, p); err != nil {
		return m.remoteOpFailed("New file failed", remoteErrText(ws.Host.Label(), err))
	}
	label, files := ws.Host.Label(), remoteFilesFor(ws)
	return func() tea.Msg {
		if _, err := fsys.Stat(context.Background(), p); err == nil {
			return remoteOpFailedMsg("New file failed", name+" already exists")
		}
		warn, err := files.writeFile(p, nil)
		if err != nil {
			return remoteOpFailedMsg("New file failed", remoteErrText(label, err))
		}
		return applyMsg(func(m *Model) tea.Cmd {
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
		})
	}
}

// remoteOpFailedMsg shows a failed file operation from its command.
func remoteOpFailedMsg(title, msg string) tea.Msg {
	return applyMsg(func(m *Model) tea.Cmd { return m.remoteOpFailed(title, msg) })
}

// remoteNewFolder makes the folder dir/name (and missing parents) in the
// container.
func (m *Model) remoteNewFolder(fsys workspace.FS, dir, name string) tea.Cmd {
	label := m.workspace().Host.Label()
	p := path.Join(dir, name)
	return func() tea.Msg {
		if err := fsys.Mkdir(context.Background(), p, 0o755); err != nil {
			return remoteOpFailedMsg("New folder failed", remoteErrText(label, err))
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.err = ""
			m.explorer.Reload()
			return nil
		})
	}
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
	return func() tea.Msg {
		ctx := context.Background()
		if _, err := fsys.Stat(ctx, to); err == nil {
			return remoteOpFailedMsg("Rename failed", value+" already exists")
		}
		if err := fsys.Rename(ctx, from, to); err != nil {
			return remoteOpFailedMsg("Rename failed", remoteErrText(label, err))
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.err = ""
			m.followRename(from, to)
			m.explorer.Reload()
			return m.refreshSearchResults()
		})
	}
}

// remoteDelete deletes paths in the container; bulk ends like a bulk
// delete (one counting toast), else like a single delete.
func (m *Model) remoteDelete(fsys workspace.FS, paths []string, bulk bool) tea.Cmd {
	label := m.workspace().Host.Label()
	remove := func(p string) error { return remoteRemove(fsys, label, p) }
	if !bulk {
		p := paths[0]
		return func() tea.Msg {
			err := remove(p)
			return applyMsg(func(m *Model) tea.Cmd { return m.finishDelete(p, err) })
		}
	}
	return func() tea.Msg {
		deleted, failed, gone := removePaths(paths, remove)
		return applyMsg(func(m *Model) tea.Cmd { return m.finishBulkDelete(deleted, failed, gone) })
	}
}

// remoteRemove deletes p through fsys; the error is one clear sentence.
func remoteRemove(fsys workspace.FS, label, p string) error {
	if err := fsys.Remove(context.Background(), p); err != nil {
		return errors.New(remoteErrText(label, err))
	}
	return nil
}

// removePath deletes a file or folder of the workspace: in the container
// for a container workspace (the error is one clear sentence), else on
// the local disk.
func (m *Model) removePath(p string) error {
	fsys, ok := m.remoteFS()
	if !ok {
		return osRemoveAll(p)
	}
	return remoteRemove(fsys, m.workspace().Host.Label(), p)
}
