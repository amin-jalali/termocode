//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Files tab (WEB-16, web UX 5.4): the laptop's open files first, then
// the workspace tree with lazy folders.

const filesView = `document.querySelector('[data-view="files"]')`

// openFiles logs in on vp and goes to the Files tab; on a desktop it is
// the middle column from the start.
func openFiles(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	if vp.Mobile {
		p.GoTab("files")
		c.Subs(t, "tree", "bufs")
	}
	return p, c
}

// No workspace on the laptop: the hub refuses the tree topic and the tab
// says so in its own words, with Try again, instead of loading forever.
func TestFilesNoWorkspace(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, Code: webproto.CodeNotFound, Msg: "No workspace is open.", Data: raw(map[string]any{"topic": "tree"})})
			p.WaitJS(filesView + `.querySelector(".refused")?.innerText.includes("No workspace is open on the laptop.")`)
			p.TapJS(filesView + `.querySelector(".refused button")`)
			c.WaitSub(t, "tree")
			p.WaitJS(filesView + `.querySelector(".loading")`)
		})
	}
}
