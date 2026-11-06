//go:build browser

package web

import (
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
