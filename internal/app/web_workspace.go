package app

import (
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The workspace's folders and the laptop's recent workspaces from the
// web (WEBP-9): names only go out; adding a folder, removing one and
// opening another workspace run the laptop's own commands after a
// laptop card (Cancel first, naming the device).

// webOpenWorkspace opens a recent workspace (a seam for tests: the real
// one restarts termocode there).
var webOpenWorkspace = (*Model).openWorkspace

func init() {
	webOps[webproto.OpWorkspaceList] = (*Model).webWorkspaceList
	webAskOps[webproto.OpWorkspaceAddFolder] = (*Model).webWorkspaceAdd
	webAskOps[webproto.OpWorkspaceRemoveFolder] = (*Model).webWorkspaceRemove
	webAskOps[webproto.OpWorkspaceOpen] = (*Model).webWorkspaceOpen
}

// webRecentWorkspaces are the recent workspaces other than this one,
// newest first.
func (m *Model) webRecentWorkspaces() []string {
	cur, _ := m.rootDir()
	entries := loadWorkspaces()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].OpenedAt.After(entries[j].OpenedAt) })
	var out []string
	for _, e := range entries {
		if e.Path != "" && e.Path != cur {
			out = append(out, e.Path)
		}
	}
	return out
}

func (m *Model) webWorkspaceList(call *webCall) tea.Cmd {
	out := webproto.WorkspaceList{Folders: []webproto.WorkspaceEntry{}, Recent: []webproto.WorkspaceEntry{}}
	for i, r := range m.explorer.Roots() {
		out.Folders = append(out.Folders, webproto.WorkspaceEntry{ID: i, Name: filepath.Base(r)})
	}
	for i, p := range m.webRecentWorkspaces() {
		out.Recent = append(out.Recent, webproto.WorkspaceEntry{ID: i, Name: filepath.Base(p)})
	}
	call.reply(out)
	return nil
}

func (m *Model) webWorkspaceAdd(call *webCall) *webAsk {
	p := call.args.(*webproto.WorkspacePathArgs).Path
	return &webAsk{
		summary: filepath.Base(p),
		title:   "Add a folder to the workspace?",
		body:    "Add the folder " + p + " to this workspace? Its files become visible to the web.",
		action:  "Add Folder",
		run:     func(m *Model) tea.Cmd { return m.addRootFromPrompt(p) },
	}
}

func (m *Model) webWorkspaceRemove(call *webCall) *webAsk {
	id := call.args.(*webproto.IDArgs).ID
	roots := m.explorer.Roots()
	if id == 0 {
		call.fail(webproto.CodeForbidden, "The workspace's own folder cannot be removed.")
		return nil
	}
	if id >= len(roots) {
		call.fail(webproto.CodeNotFound, "That folder is not in the workspace any more.")
		return nil
	}
	root := roots[id]
	return &webAsk{
		summary: filepath.Base(root),
		title:   "Remove a folder from the workspace?",
		body:    "Remove the folder " + filepath.Base(root) + " from this workspace? Its files stay on disk.",
		action:  "Remove Folder",
		run:     func(m *Model) tea.Cmd { return m.removeRootFromPicker(root) },
	}
}

func (m *Model) webWorkspaceOpen(call *webCall) *webAsk {
	id := call.args.(*webproto.IDArgs).ID
	recent := m.webRecentWorkspaces()
	if id >= len(recent) {
		call.fail(webproto.CodeNotFound, "That workspace is not in the recent list any more.")
		return nil
	}
	p := recent[id]
	return &webAsk{
		summary: filepath.Base(p),
		title:   "Open another workspace?",
		body:    "Open the workspace " + filepath.Base(p) + "? termocode restarts there and this web session ends.",
		action:  "Open Workspace",
		run:     func(m *Model) tea.Cmd { return webOpenWorkspace(m, p) },
	}
}
