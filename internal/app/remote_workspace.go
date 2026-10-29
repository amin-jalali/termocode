package app

import (
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// remoteFS is the Host FS of a workspace that is not on this machine (a
// container, ADR 0018). ok is false for a local folder: code there keeps
// using the os package.
func remoteFS(ws workspace.Workspace) (workspace.FS, bool) {
	if isLocal(ws) || ws.Host == nil {
		return nil, false
	}
	return ws.Host.FS(), true
}

// remoteFS is the Host FS of the Model's workspace (see remoteFS).
func (m Model) remoteFS() (workspace.FS, bool) { return remoteFS(m.workspace()) }

// newExplorerFor is the explorer of ws: the local folder, or the
// workspace root listed through the Host FS.
func newExplorerFor(ws workspace.Workspace) explorer.Model {
	if fsys, ok := remoteFS(ws); ok {
		return explorer.NewFS(fsys, ws.Root)
	}
	return explorer.New()
}
