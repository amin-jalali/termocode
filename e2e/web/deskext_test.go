//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

const extRows = `[...document.querySelectorAll('.wb-side [data-view="extensions"] .tr')]`

// Lua extension panels and status items (WEBP-7): the laptop's status
// items sit in the status bar (one with a command is a button that runs
// it); its panels are one Extensions view in the activity bar, each a
// section with the panel's styled lines, and a row picked runs the
// panel's select handler on the laptop. No extensions, no item.
func TestDeskExtensionPanelsAndStatusItems(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.WaitJS(`document.querySelector(".act-bar") && !document.querySelector('.act-bar [data-act="extensions"]')`)
	st := statusData("main", 0)
	st["ext"] = map[string]any{
		"panels": []any{map[string]any{"id": "good.main", "title": "Good", "ext": "good"}},
		"items":  []any{map[string]any{"id": "good.st", "text": "st ok", "run": true}, map[string]any{"id": "good.clock", "text": "12:00"}},
	}
	c.Send(t, Snap("status", 2, st))
	p.WaitJS(`[...document.querySelectorAll(".statusbar .sb-ext")].map((e) => e.tagName + ":" + e.textContent).join() === "BUTTON:st ok,SPAN:12:00"`)
	p.TapJS(`document.querySelector('.statusbar .sb-ext[data-ext="good.st"]')`)
	reply(t, c, waitArgs(t, c, webproto.OpExtStatusRun, `{"id":"good.st"}`), nil)

	p.TapAct("extensions")
	r := waitArgs(t, c, webproto.OpExtPanel, `{"id":"good.main"}`)
	reply(t, c, r, webproto.ExtPanelView{Lines: [][]webproto.ExtSpan{{{T: "width 60", S: "accent bold"}, {T: " rest"}}, {{T: "row two"}}}})
	p.WaitJS(`document.querySelector('.wb-side [data-view="extensions"] .ps-title')?.textContent === "Good"`)
	p.WaitJS(extRows + `.map((e) => e.textContent).join("|") === "width 60 rest|row two"`)
	p.WaitJS(`document.querySelector('.wb-side [data-view="extensions"] .xm-accent.xm-bold')?.textContent === "width 60"`)
	p.TapJS(extRows + `[1]`)
	reply(t, c, waitArgs(t, c, webproto.OpExtSelect, `{"id":"good.main","row":1}`), nil)
}
