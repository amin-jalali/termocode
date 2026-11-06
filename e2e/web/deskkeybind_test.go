//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Preferences: Customize Keybindings (WEBP-7): the laptop's actions with
// their keys in a picker; one picked takes a key typed here. A key
// another action has is asked here first (Cancel first) and only then
// taken from it.
func TestDeskCustomizeKeybindings(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "customize keybindings", "Preferences: Customize Keybindings")
	reply(t, c, waitReq(t, c, webproto.OpKeysList), webproto.KeyBindings{Items: []webproto.KeyBinding{{Action: "PickTheme", Keys: []string{}}, {Action: "Save", Keys: []string{"ctrl+s", "f2"}}}})
	p.WaitJS(`[...document.querySelectorAll(".quick-row")].map((e) => e.querySelector(".quick-label").textContent + "=" + (e.querySelector(".quick-detail")?.textContent ?? "")).join() === "PickTheme=no key,Save=ctrl+s, f2"`)
	p.Key("picktheme")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "PickTheme"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Laptop key for PickTheme (ctrl+x, f5, alt+enter)" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("ctrl+s")
	p.Key("\r")
	r := waitArgs(t, c, webproto.OpKeysBind, `{"action":"PickTheme","key":"ctrl+s"}`)
	reply(t, c, r, webproto.KeyBound{Conflict: "Save"})
	p.WaitJS(`document.querySelector(".ask-here .ask-text")?.textContent === "ctrl+s is the key of Save now. Give it to PickTheme? Save loses this key."`)
	p.WaitJS(`document.activeElement === document.querySelector(".ask-here .choices button")`)
	p.WaitJS(`document.querySelector(".ask-here .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask-here .btn-danger")`)
	r = waitArgs(t, c, webproto.OpKeysBind, `{"action":"PickTheme","key":"ctrl+s","replace":true}`)
	reply(t, c, r, webproto.KeyBound{Saved: true})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("PickTheme: ctrl+s on the laptop.")`)
}
