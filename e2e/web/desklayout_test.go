//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func deskLayouts() webproto.LayoutList {
	return webproto.LayoutList{Items: []webproto.LayoutEntry{
		{Name: "focus", Source: "built-in", Description: "Editor only. No sidebar, panel or chat.", Preview: []string{"Sidebar: hidden"}},
		{Name: "mine", Source: "user", Preview: []string{"Sidebar: Source Control", "Chat panel: open"}},
	}}
}

// Layout: Open… and Reset to Default (WEBP-7): the laptop's layouts in a
// picker (where each comes from and what it changes); one picked opens
// on the laptop.
func TestDeskLayoutOpen(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "layout open", "Layout: Open…")
	reply(t, c, waitReq(t, c, webproto.OpLayoutList), deskLayouts())
	p.WaitJS(`[...document.querySelectorAll(".quick-row")].map((e) => e.querySelector(".quick-label").textContent + "=" + (e.querySelector(".quick-detail")?.textContent ?? "")).join("|") === "focus=built-in · Editor only. No sidebar, panel or chat.|mine=user · Sidebar: Source Control · Chat panel: open"`)
	p.Key("mine")
	pickRow(p, "mine")
	reply(t, c, waitArgs(t, c, webproto.OpLayoutOpen, `{"name":"mine"}`), nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Layout mine is open on the laptop.")`)

	runCmd(t, p, c, false, "layout reset", "Layout: Reset to Default")
	reply(t, c, waitArgs(t, c, webproto.OpLayoutReset, `{}`), nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("The laptop shows the default layout.")`)
}

// Layout: Save As… and Delete… (WEBP-7): a name typed here, then who
// gets it; a name that is taken is asked here (Cancel first) before it
// is replaced. Delete lists only saved layouts and waits for the laptop
// card.
func TestDeskLayoutSaveAndDelete(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "layout save", "Layout: Save As…")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Name for the laptop's layout now" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("mine")
	p.Key("\r")
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "Only me,This project"`)
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Only me"`)
	p.Key("\r")
	r := waitArgs(t, c, webproto.OpLayoutSave, `{"name":"mine","scope":"user"}`)
	reply(t, c, r, webproto.LayoutSaved{Exists: true})
	p.WaitJS(`document.querySelector(".ask-here .ask-text")?.textContent === "A layout named mine exists. Replace it with the laptop's layout now?"`)
	p.WaitJS(`document.activeElement === document.querySelector(".ask-here .choices button")`)
	p.WaitJS(`document.querySelector(".ask-here .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask-here .btn-danger")`)
	r = waitArgs(t, c, webproto.OpLayoutSave, `{"name":"mine","scope":"user","replace":true}`)
	reply(t, c, r, webproto.LayoutSaved{Saved: true})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Layout mine saved.")`)

	runCmd(t, p, c, false, "layout delete", "Layout: Delete…")
	reply(t, c, waitReq(t, c, webproto.OpLayoutList), deskLayouts())
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "mine"`)
	pickRow(p, "mine")
	if got := p.Text(".ask .ask-text"); got != "Delete the layout mine on the laptop? It cannot be restored there." {
		t.Errorf("question %q", got)
	}
	askTheLaptop(t, p, c, webproto.OpLayoutDelete, `{"name":"mine"}`)
}
