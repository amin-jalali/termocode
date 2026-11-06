//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Search: Replace in Workspace… (WEBP-9, Editor and files): the text to
// find and its replacement are typed here; the laptop's card counts the
// matches and only its yes writes the files.
func TestDeskReplaceInWorkspace(t *testing.T) {
	p, c := openFiles(t, Desktop)
	wbKey(p, "P", true, true)
	answerReqs(t, c, map[string]any{"palette.list": webproto.PaletteList{Items: []webproto.PaletteItem{}}})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("replace in workspace")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Search: Replace in Workspace…"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Find in every file" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("oldName")
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Replace oldName with" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("newName")
	p.Key("\r")
	if got := p.Text(".ask .ask-text"); got != "Replace oldName with newName in every file of the workspace?" {
		t.Errorf("question %q", got)
	}
	askTheLaptop(t, p, c, webproto.OpSearchReplace, `{"find":"oldName","replace":"newName"}`)
}
