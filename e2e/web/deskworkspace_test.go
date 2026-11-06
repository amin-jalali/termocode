//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The workspace's folders from the desktop (WEBP-9, Editor and files):
// Add Folder… takes a path typed here, Remove Folder… and Open Recent
// Workspace… pick from the laptop's names; each asks here first (Cancel
// first) and then waits for the laptop card.

// askTheLaptop waits for the in-page question, presses its armed action
// and checks the request it sends.
func askTheLaptop(t *testing.T, p *Page, c *HubConn, op, args string) {
	t.Helper()
	p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask .btn-danger")`)
	r := waitArgs(t, c, op, args)
	reply(t, c, r, webproto.AskRef{Card: "k-" + op})
	p.WaitJS(`document.querySelector(".waiting")`)
}

func TestDeskWorkspaceFolders(t *testing.T) {
	p, c := openFiles(t, Desktop)
	wbKey(p, "P", true, true)
	answerReqs(t, c, map[string]any{"palette.list": webproto.PaletteList{Items: []webproto.PaletteItem{}}})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("add folder")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Workspace: Add Folder…"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "The folder's full path on the laptop" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("/home/me/lib")
	p.Key("\r")
	if got := p.Text(".ask .ask-text"); got != "Add the folder /home/me/lib to this workspace on the laptop?" {
		t.Errorf("question %q", got)
	}
	askTheLaptop(t, p, c, webproto.OpWorkspaceAddFolder, `{"folder":"/home/me/lib"}`)
	p.TapJS(`document.querySelector(".waiting .btn")`)
	waitReq(t, c, webproto.OpCardsAnswer)

	runCmd(t, p, c, false, "remove folder", "Workspace: Remove Folder…")
	r := waitReq(t, c, webproto.OpWorkspaceList)
	reply(t, c, r, webproto.WorkspaceList{Folders: []webproto.WorkspaceEntry{{ID: 0, Name: "ws-root"}, {ID: 1, Name: "lib"}}, Recent: []webproto.WorkspaceEntry{}})
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "lib"`)
	pickRow(p, "lib")
	askTheLaptop(t, p, c, webproto.OpWorkspaceRemoveFolder, `{"id":1}`)
}

func TestDeskOpenRecentWorkspace(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "recent workspace", "File: Open Recent Workspace…")
	r := waitReq(t, c, webproto.OpWorkspaceList)
	reply(t, c, r, webproto.WorkspaceList{Folders: []webproto.WorkspaceEntry{{ID: 0, Name: "ws-root"}}, Recent: []webproto.WorkspaceEntry{{ID: 0, Name: "other-app"}}})
	pickRow(p, "other-app")
	if got := p.Text(".ask .ask-text"); got != "Open the workspace other-app on the laptop? termocode restarts there and this web session ends." {
		t.Errorf("question %q", got)
	}
	askTheLaptop(t, p, c, webproto.OpWorkspaceOpen, `{"id":0}`)
}

// WEBP-8: a folder added to the workspace shows as its own root under
// the workspace's, with its name as the group title, and lists lazily as
// the workspace's own folder does. A roots delta that drops it takes it
// (and its open folders) away.
func TestDeskAddedFoldersShowInTheExplorer(t *testing.T) {
	p, c := openFiles(t, Desktop)
	snap := treeSnap(false)
	snap["roots"] = []any{map[string]any{"path": "", "label": "ws-root"}, map[string]any{"path": ":lib", "label": "lib"}}
	c.Send(t, Snap("tree", 1, snap))
	expectList(t, c, "tree.list", "")
	expectList(t, c, "tree.list", ":lib")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("a.go", false))))
	c.Send(t, Delta("tree", 3, dirDelta(":lib", false, entry("sub", true), entry("x.go", false))))
	p.WaitJS(treeRow(":lib/x.go"))
	groups := Eval[[]string](p, `[...`+filesView+`.querySelectorAll(".tree-group .group-title")].map((e) => e.textContent)`)
	if strings.Join(groups, "|") != "ws-root|lib" {
		t.Errorf("groups %q, want the workspace, then the added folder", groups)
	}
	p.TapJS(treeRow(":lib/sub"))
	expectList(t, c, "tree.list", ":lib/sub")
	c.Send(t, Delta("tree", 4, dirDelta(":lib/sub", false, entry("y.go", false))))
	p.WaitJS(treeRow(":lib/sub/y.go"))

	c.Send(t, Delta("tree", 5, map[string]any{"kind": "roots", "roots": []any{map[string]any{"path": "", "label": "ws-root"}}}))
	p.WaitJS(`!` + treeRow(":lib/x.go") + ` && [...` + filesView + `.querySelectorAll(".tree-group .group-title")].length === 1`)
}

// WEBP-8: the remote item at the start of the status bar opens the
// workspace's actions, as the laptop's project switcher does: open a
// recent workspace, add or remove a folder.
func TestDeskStatusBarSwitchesWorkspace(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.TapJS(`document.querySelector(".statusbar .sb-remote")`)
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join("|") === "File: Open Recent Workspace…|Workspace: Add Folder…|Workspace: Remove Folder…"`)
	pickRow(p, "File: Open Recent Workspace…")
	r := waitReq(t, c, webproto.OpWorkspaceList)
	reply(t, c, r, webproto.WorkspaceList{Folders: []webproto.WorkspaceEntry{{ID: 0, Name: "ws-root"}}, Recent: []webproto.WorkspaceEntry{{ID: 0, Name: "other-app"}}})
	pickRow(p, "other-app")
	askTheLaptop(t, p, c, webproto.OpWorkspaceOpen, `{"id":0}`)
}

// WEBP-8: a newer release shows as the laptop's update chip; a click
// says where to update (on the laptop: the web never updates the
// binary).
func TestDeskUpdateChip(t *testing.T) {
	p, c := openFiles(t, Desktop)
	if Eval[bool](p, `document.querySelector(".statusbar .sb-update") !== null`) {
		t.Fatal("a chip with no newer release")
	}
	st := statusData("main", 0)
	st["update"] = "v0.2.0"
	c.Send(t, Snap("status", 2, st))
	p.WaitJS(`document.querySelector(".statusbar .sb-update")?.textContent === "v0.2.0"`)
	p.TapJS(`document.querySelector(".statusbar .sb-update")`)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("termocode v0.2.0 is out") && document.querySelector(".toasts")?.textContent.includes("termocode: Check for Updates")`)
}
