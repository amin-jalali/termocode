//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Tactile Code keyboard (TK-1) on a phone: the editor's focus target
// gets inputmode="none" and the on-screen keyboard docks at the bottom;
// its keys type with auto pairs, the strip suggests the buffer's words,
// the dock hides and shows, and its globe key hands over to the system
// keyboard.

const tkDock = `document.querySelector(".tk-dock")`

func tkKey(id string) string {
	return `document.querySelector('.tk-dock:not([hidden]) .tk-key[data-key=` + jsq(id) + `]')`
}

func tapKeys(p *Page, ids ...string) {
	p.T.Helper()
	for _, id := range ids {
		p.TapJS(tkKey(id))
	}
}

func TestTactileKeyboardTypesInTheEditor(t *testing.T) {
	p, c := openFiles(t, Phone)
	openBuf(t, p, c, 2, "main.go", "alpha", "")
	openEditor(t, p)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden`)
	if !slicesHas(p.Requests(), "/assets/tactile.js") || !slicesHas(p.Requests(), "/assets/tactile.css") {
		t.Fatalf("the keyboard files did not load: %v", p.Requests())
	}
	if got := Eval[string](p, cmContent+`.getAttribute("inputmode")`); got != "none" {
		t.Errorf("the editor's inputmode is %q, want none (no system keyboard)", got)
	}
	if !Eval[bool](p, `document.documentElement.classList.contains("tk-on")`) {
		t.Error("html has no tk-on while the keyboard is up")
	}
	// The shell ends above the dock; the tab bar hides while it is up.
	if !Eval[bool](p, `Math.abs(document.querySelector(".shell").getBoundingClientRect().bottom - document.querySelector(".tk-host").getBoundingClientRect().top) < 1.5`) {
		t.Error("the shell does not end above the keyboard")
	}
	if Eval[bool](p, `!!document.querySelector(".tabbar")?.offsetHeight`) {
		t.Error("the tab bar shows under the keyboard")
	}
	// The compact phone layout: no function row.
	if Eval[bool](p, `!!document.querySelector('.tk-key[data-key="f1"]')`) {
		t.Error("the phone shows the function row")
	}
	// Down to the empty row; shift, 9 → "(" with its pair, the caret inside.
	tapKeys(p, "down", "X", "shift", "9", "Y")
	p.WaitJS(`(` + cmHost + `.shadowRoot.querySelectorAll(".cm-line")[1]?.textContent ?? "") === "x(y)"`)
	// Past the closer, a new row; the strip offers the buffer's words.
	tapKeys(p, "right", "enter", "A", "L")
	p.WaitJS(`[...document.querySelectorAll(".tk-sugg .tk-chip-label")].some((c) => c.textContent === "alpha")`)
	p.TapJS(`[...document.querySelectorAll(".tk-sugg .tk-chip")].find((c) => c.querySelector(".tk-chip-label").textContent === "alpha")`)
	p.WaitJS(`(` + cmHost + `.shadowRoot.querySelectorAll(".cm-line")[2]?.textContent ?? "") === "alpha"`)
	if got := edText(p); got != "alpha\nx(y)\nalpha" {
		t.Errorf("editor text %q", got)
	}
	// Hide → the Show keyboard bar → back.
	p.TapJS(`document.querySelector(".tk-hide")`)
	p.WaitJS(`!document.querySelector(".tk-dock .tk-collapsed").hidden && document.documentElement.classList.contains("tk-off")`)
	p.TapJS(`document.querySelector(".tk-dock .tk-collapsed")`)
	p.WaitJS(`document.querySelector(".tk-dock .tk-collapsed").hidden && document.documentElement.classList.contains("tk-on")`)
	// The globe key: the system keyboard takes over (Persian, dictation).
	p.TapJS(`document.querySelector(".tk-system")`)
	p.WaitJS(tkDock + `.hidden && ` + cmContent + `.getAttribute("inputmode") === "text"`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// This phone → Keyboard off: the editor keeps the system keyboard and the
// keyboard's files never load.
func TestTactileKeyboardOffKeepsTheSystemKeyboard(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.onNewDocument(`localStorage.setItem("termocode.tactile.v1", JSON.stringify({on: false}))`)
	c := p.Login("c-1")
	p.GoTab("files")
	c.Subs(t, "tree", "bufs")
	openBuf(t, p, c, 2, "main.go", "alpha")
	openEditor(t, p)
	if slicesHas(p.Requests(), "/assets/tactile.js") {
		t.Error("the keyboard loaded while it is off")
	}
	if Eval[bool](p, cmContent+`.hasAttribute("inputmode")`) {
		t.Error("the editor got an inputmode while the keyboard is off")
	}
}

// This phone → Keyboard: the Tactile keyboard's three settings, kept on
// this device (termocode.tactile.v1), defaults on / on / medium.
func TestTactileKeyboardSettingsOnThisPhone(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	Eval[bool](p, `(location.hash = "#/more/phone", true)`)
	r := expectReq(t, c, "phone.get")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"device": "c-1", "name": "phone", "role": "controller", "passkey": true, "paired_ms": 1, "prefs": map[string]any{}, "laptops": []any{}})})
	group := `document.querySelector('[aria-labelledby="ph-kb"]')`
	p.WaitJS(group)
	pressed := func(label string) string {
		return Eval[string](p, `[...`+group+`.querySelectorAll(".field")].find((f) => f.querySelector(".field-label").textContent === `+jsq(label)+`).querySelector('[aria-checked="true"], [aria-pressed="true"]')?.textContent ?? ""`)
	}
	if a, b, v := pressed("Tactile keyboard"), pressed("Key sound"), pressed("Key vibration"); a != "On" || b != "On" || v != "Medium" {
		t.Fatalf("defaults %q %q %q, want On On Medium", a, b, v)
	}
	p.TapJS(`[...` + group + `.querySelectorAll("button")].find((b) => b.textContent === "Strong")`)
	p.TapJS(`[...` + group + `.querySelectorAll(".field")][1].querySelector("button")`) // Key sound: Off
	p.WaitJS(`JSON.parse(localStorage.getItem("termocode.tactile.v1") ?? "{}").haptic === 3`)
	if got := Eval[string](p, `localStorage.getItem("termocode.tactile.v1")`); got != `{"on":true,"sound":false,"haptic":3}` {
		t.Errorf("stored %s", got)
	}
}
