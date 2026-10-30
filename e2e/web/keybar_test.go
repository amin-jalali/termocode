//go:build browser

package web

import (
	"strings"
	"testing"
)

// The custom key bar (WEB-56): the user picks the bar's keys from a
// fixed list, orders them and goes back to the default; the choice is
// kept on this device as key names only, never as text.

func barKeys(p *Page) []string {
	return Eval[[]string](p, `[...document.querySelectorAll('[data-view="term"] .keybar .key')].map((k) => k.textContent)`)
}

const keysSheet = `document.querySelector(".keys-edit")`

func keysRow(label string) string {
	return `[...document.querySelectorAll(".keys-edit .keys-row")].find((r) => r.querySelector(".keys-name").textContent === ` + jsq(label) + `)`
}

func TestKeyBarCustomRowPersists(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTerm(t, vp)
			showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
			def := barKeys(p)
			if strings.Join(def, " ") != "Esc Tab Ctrl Alt ← ↓ ↑ → ^C ⋯ Paste" {
				t.Fatalf("default bar %q", def)
			}
			p.TapJS(`document.querySelector('[data-view="term"] .keybar .key-edit')`)
			p.WaitJS(keysSheet)
			p.TapJS(keysRow("Tab") + `.querySelector(".keys-remove")`)
			p.TapJS(`[...document.querySelectorAll(".keys-edit .keys-add")].find((b) => b.textContent === "^D")`)
			for range 9 { // from the end to the top
				p.TapJS(keysRow("^D") + `.querySelector(".keys-up")`)
			}
			p.WaitJS(`document.querySelector(".keys-edit .keys-row .keys-name").textContent === "^D"`)
			if !Eval[bool](p, keysRow("^D")+`.querySelector(".keys-up").disabled`) {
				t.Error("the first key can still move up")
			}
			p.TapJS(ButtonByText("Done"))
			p.WaitJS(`!` + keysSheet)
			got := strings.Join(barKeys(p), " ")
			if got != "^D Esc Ctrl Alt ← ↓ ↑ → ^C ⋯ Paste" {
				t.Errorf("bar %q", got)
			}
			if !Eval[bool](p, `JSON.stringify(JSON.parse(localStorage.getItem("termocode.prefs.v1")).keys) === JSON.stringify(["ctrl_d","esc","ctrl","alt","left","down","up","right","int"])`) {
				t.Errorf("stored keys %s", Eval[string](p, `JSON.stringify(JSON.parse(localStorage.getItem("termocode.prefs.v1")).keys)`))
			}
			tapKey(p, "^D")
			if got := expectSend(t, c); got != "\x04" {
				t.Errorf("^D sent %q", got)
			}

			p.TapJS(`document.querySelector('[data-view="term"] .keybar .key-edit')`)
			p.TapJS(ButtonByText("Default keys"))
			p.TapJS(ButtonByText("Done"))
			p.WaitJS(`!` + keysSheet)
			if got := strings.Join(barKeys(p), " "); got != strings.Join(def, " ") {
				t.Errorf("after Default keys: %q", got)
			}
		})
	}
}

// Stored keys are read back as names from the fixed list only: anything
// else (an unknown name, text, a token) is dropped.
func TestKeyBarDropsUnknownStoredKeys(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.onNewDocument(`localStorage.setItem("termocode.prefs.v1", JSON.stringify({keys: ["home", "sk-` + `test-0000", "home", "<b>x</b>", 7, "pgdn"]}))`)
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	if got := strings.Join(barKeys(p), " "); got != "Home PgDn ⋯ Paste" {
		t.Errorf("bar %q", got)
	}
	tapKey(p, "PgDn")
	if got := expectSend(t, c); got != "\x1b[6~" {
		t.Errorf("PgDn sent %q", got)
	}
}
