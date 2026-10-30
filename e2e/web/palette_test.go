//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The command palette (Ctrl/Cmd+Shift+P) lists the laptop palette's safe
// commands beside the page's own, with their keys; it runs one on the
// laptop; a risky one asks the laptop first; what ran last comes first.

func laptopCmds() webproto.PaletteList {
	return webproto.PaletteList{Items: []webproto.PaletteItem{
		{ID: "save-all", Title: "File: Save All Files"},
		{ID: "git-fetch", Title: "Git: Fetch", Keys: "Alt+F"},
		{ID: "git-push", Title: "Git: Push", Ask: true},
	}}
}

func TestPaletteRunsLaptopCommands(t *testing.T) {
	p, c := openFiles(t, Desktop)
	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), laptopCmds())
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("save all")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "File: Save All Files"`)
	if got := p.Text(".quick-row.is-on .quick-detail"); got != "Laptop" {
		t.Errorf("a laptop command's note is %q", got)
	}
	p.Key("\r")
	reply(t, c, expectOp(t, c, "palette.run", `{"id":"save-all"}`), nil)
	p.WaitJS(`!document.querySelector(".quick")`)

	// The laptop's keys show beside its command.
	wbKey(p, "P", true, true)
	p.WaitJS(`document.querySelector(".quick .quick-box")?.value === ">" && document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("git fetch")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-keys")?.textContent === "Alt+F"`)
	p.Key(kb.Escape)

	// What ran last is first, marked "recently used".
	wbKey(p, "P", true, true)
	p.WaitJS(`document.querySelector(".quick-row .quick-label")?.textContent === "File: Save All Files" && document.querySelector(".quick-row .quick-tag")?.textContent === "recently used" && document.activeElement === document.querySelector(".quick .quick-box")`)

	// A risky command asks the laptop: first here (Cancel first), then there.
	p.Key("git push")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Git: Push"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "palette.ask", `{"id":"git-push"}`), webproto.AskRef{Card: "card-3"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// A viewer reads only: the palette lists no laptop command and asks for none.
func TestPaletteViewerHasNoLaptopCommands(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	m := Meta("c-1")
	m.Role = webproto.RoleViewer
	p.LoginMeta(m)
	wbKey(p, "P", true, true)
	p.WaitJS(`document.querySelector(".quick .quick-box")?.value === ">" && document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("save")
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].every((e) => e.textContent !== "File: Save All Files")`)
}
