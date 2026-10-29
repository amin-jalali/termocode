package app

import (
	"context"
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
