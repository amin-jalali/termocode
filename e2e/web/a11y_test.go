//go:build browser

package web

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp/kb"
)

// The accessibility pass (WEB-47, web UX 8.1): axe-core, vendored for
// the tests only, checks every view and the sheets on the phone and the
// desktop, dark and light, against WCAG 2.2 A and AA.

const axeDir = "testdata/axe-core@4.10.3"

// axeSource reads axe.min.js after every vendored file matches its hash.
func axeSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(axeDir, "axe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(b, &rec); err != nil || len(rec.Files) == 0 {
		t.Fatalf("axe.json: %v", err)
	}
	for name, want := range rec.Files {
		body, err := os.ReadFile(filepath.Join(axeDir, name))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha512.Sum384(body)
		if got := "sha384-" + base64.StdEncoding.EncodeToString(sum[:]); got != want {
			t.Fatalf("%s/%s does not match axe.json", axeDir, name)
		}
	}
	src, _ := os.ReadFile(filepath.Join(axeDir, "axe.min.js"))
	return string(src)
}

// axe runs the checker on the page as it is now and fails the test on
// each violation, named by where.
func (p *Page) axe(src, where string) { p.T.Helper(); p.axeWith(src, where, "") }

// axeDense is axe for the desktop's dense lists: their rows and icon
// buttons keep the laptop editor's 22 px (the owner's choice for the
// desktop, where the pointer is fine and the tree keys reach every
// row), so the 24 px target size rule is off; every other rule holds.
func (p *Page) axeDense(src, where string) {
	p.T.Helper()
	p.axeWith(src, where, `rules: {"target-size": {enabled: false}},`)
}

func (p *Page) axeWith(src, where, extra string) {
	p.T.Helper()
	if !Eval[bool](p, `typeof window.axe === "object"`) {
		Eval[bool](p, src+`; true`)
	}
	Eval[bool](p, `(window.__axe = undefined, axe.run(document, {
		runOnly: {type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]},
		resultTypes: ["violations"],
		`+extra+`
	}).then((r) => (window.__axe = r.violations.map((v) => v.id + " (" + v.impact + "): " + v.help + " @ " + v.nodes.slice(0, 3).map((n) => n.target.join(" ")).join(" | "))),
		(e) => (window.__axe = ["axe failed: " + e])), true)`)
	p.WaitJS(`window.__axe !== undefined`)
	if bad := Eval[[]string](p, `window.__axe`); len(bad) > 0 {
		p.T.Errorf("%s %s: %d accessibility problems:\n  %s", p.VP.Name, where, len(bad), strings.Join(bad, "\n  "))
	}
}

func TestAxeEveryView(t *testing.T) {
	src := axeSource(t)
	for _, vp := range Both {
		for _, theme := range []string{"dark", "light"} {
			t.Run(vp.Name+"/"+theme, func(t *testing.T) {
				s := StartServer(t)
				p := NewPage(t, s, vp)
				p.Media(map[string]string{"prefers-color-scheme": theme, "prefers-reduced-motion": "reduce"})
				p.Open()
				p.axe(src, "connect")
				c := p.LoginHere("c-1")
				c.Send(t, Snap("cards", 1, cardsData(toolCard("card-1", "go test ./..."))))
				p.axe(src, "chat")
				p.GoTab("term")
				c.WaitSub(t, "terms")
				showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a b c"), termItem(3, "zsh", true, true), termItem(4, "build", false, false))
				p.axe(src, "term")
				p.Tap(`.term-info-btn`)
				p.WaitJS(`document.querySelector(".term-about")`)
				p.axe(src, "term about sheet")
				p.Key(kb.Escape)
				if vp.Mobile { // the desktop has no key bar
					p.TapJS(`document.querySelector('[data-view="term"] .keybar .key-edit')`)
					p.WaitJS(`document.querySelector(".keys-edit")`)
					p.axe(src, "key bar editor")
					p.TapJS(ButtonByText("Done"))
					p.WaitJS(`!document.querySelector(".keys-edit")`)
				}
				for _, tab := range []string{"files", "git", "more"} {
					p.GoTab(tab)
					p.axe(src, tab)
				}
				p.Tap(".attention")
				p.WaitJS(`document.querySelector(".card")`)
				p.axe(src, "card sheet")
			})
		}
	}
}

// The terminal's rows do not speak each new line (too noisy): the log is
// not live. A button, shown when it has the keyboard focus, reads the
// last 20 rows aloud through a status region.
func TestTermReadLastRows(t *testing.T) {
	p, c := openTerm(t, Desktop)
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = "row " + string(rune('a'+i%26)) + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	showTerm(t, p, c, termSnap(3, 0, 80, rows...), termItem(3, "zsh", false, true))
	if got := Eval[string](p, `document.querySelector('[data-view="term"] .term-rows').getAttribute("aria-live") ?? ""`); got != "off" {
		t.Errorf("term rows aria-live %q, want off", got)
	}
	read := `document.querySelector('[data-view="term"] .term-read')`
	if w := Eval[float64](p, read+`.getBoundingClientRect().width`); w > 1 {
		t.Errorf("the read button shows (%v px wide) without focus", w)
	}
	Eval[bool](p, read+`.focus() || true`)
	if w := Eval[float64](p, read+`.getBoundingClientRect().width`); w < 44 {
		t.Errorf("the focused read button is %v px wide", w)
	}
	p.Key("\r")
	p.WaitJS(`document.querySelector('[data-view="term"] .term-said').textContent === ` + jsq(strings.Join(rows[10:], "\n")))
	if Eval[string](p, `document.querySelector('[data-view="term"] .term-said').getAttribute("role")`) != "status" {
		t.Error("the read rows are not in a status region")
	}
}

// The lost-connection bar counts down on screen, but a screen reader
// hears it once: its status text does not change every second.
func TestOfflineBarSpeaksOnce(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	s.Hub.Refuse(true)
	c.Drop()
	p.WaitJS(`document.querySelector(".bar-lost")`)
	spoken := `[...document.querySelectorAll('.bar-lost [role="status"], .bar-lost[role="status"]')].map((e) => [...e.childNodes].filter((n) => !(n.nodeType === 1 && n.getAttribute("aria-hidden") === "true")).map((n) => n.textContent).join("")).join("|")`
	first := Eval[string](p, spoken)
	if first == "" || strings.ContainsAny(first, "0123456789") {
		t.Errorf("the bar's spoken text %q changes with the countdown", first)
	}
	if !Eval[bool](p, `/\d/.test(document.querySelector(".bar-lost").textContent)`) {
		t.Error("the countdown is not on screen")
	}
}

// Reduced motion stops every endless animation (the ✦ pulse, the busy
// comet); more contrast raises the dim text; forced colours keep the
// bars' edges and the focus ring.
func TestMotionAndContrastPreferences(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	st := statusData("main", 0)
	st["ai"] = map[string]any{"provider_label": "AI", "model": "m", "busy": true, "cards_waiting": 0}
	c.Send(t, Snap("status", 2, st))
	p.WaitJS(`document.querySelector(".strip .ai-working")`)
	endless := `document.getAnimations().filter((a) => a.playState === "running" && a.effect && a.effect.getComputedTiming().iterations === Infinity).length`
	if n := Eval[float64](p, endless); n != 0 {
		t.Errorf("%v endless animations run with reduced motion", n)
	}

	p.Media(map[string]string{"prefers-contrast": "more"})
	if !Eval[bool](p, `(() => { const s = getComputedStyle(document.documentElement); return s.getPropertyValue("--dim").trim() === s.getPropertyValue("--muted").trim(); })()`) {
		t.Error("more contrast does not raise --dim (light system theme)")
	}

	p.Media(map[string]string{"forced-colors": "active"})
	if got := Eval[string](p, `getComputedStyle(document.querySelector(".tabbar")).borderTopStyle`); got != "solid" {
		t.Errorf("forced colours: tab bar edge %q", got)
	}
	Eval[bool](p, `document.querySelector("nav .nav-item").focus() || true`)
	p.Key("\t")
	p.Key("\t")
	if got := Eval[string](p, `getComputedStyle(document.activeElement).outlineStyle`); got == "none" {
		t.Error("forced colours: no focus ring")
	}
}

// The Tab key walks the page the way it reads: "Skip to content" first,
// then the title bar (desktop) or the strip, the activity bar, the side
// bar, the editor, the panel, the chat column (desktop), the hint line
// and the status bar (desktop) and the tab bar (phone); inside each, top to bottom and left to right.
// A sash goes with the pane it resizes. The skip link moves the focus
// into the view.
func TestFocusOrderFollowsTheScreen(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			p.Login("c-1")
			p.GoTab("more")
			if !Eval[bool](p, `document.querySelector("a[href], button, input, textarea, select, [tabindex]:not([tabindex='-1'])") === document.querySelector(".skip")`) {
				t.Error("the skip link is not the first stop")
			}
			Eval[bool](p, `(document.querySelector(".skip").focus(), true)`)
			type stop struct {
				Name      string
				Top, Left float64
				Region    int
				Sash      bool
			}
			var stops []stop
			for i := range 40 {
				if i > 0 {
					p.Key("\t")
				}
				// Positions are in page terms: a stop that scrolls its view
				// into sight must not look like a step back up.
				stops = append(stops, Eval[stop](p, `(() => { const e = document.activeElement; const r = e.getBoundingClientRect();
					let top = r.top + scrollY, left = r.left + scrollX;
					for (let x = e.parentElement; x; x = x.parentElement) { top += x.scrollTop; left += x.scrollLeft; }
					const sash = !e.classList.contains("sash") ? null : e.previousElementSibling?.matches(".wb-side") ? e.previousElementSibling : e.nextElementSibling;
					const at = sash ?? e;
					const region = e === document.body || e === document.documentElement ? 99 : at.closest(".skip") ? 0 : at.closest(".statusbar, .layout-desktop .hint") ? 7 : at.closest(".titlebar, .strip, .bars") ? 1 : at.closest("nav.rail") ? 2 : at.closest(".wb-side") ? 3 : at.closest("main, .wb-editor") ? 4 : at.closest(".wb-panel") ? 5 : at.closest(".chatcol") ? 6 : at.closest(".statusbar") ? 7 : at.closest(".pill-wrap") ? 8 : at.closest("nav.tabbar") ? 9 : 98;
					return {Name: (e.getAttribute("aria-label") || e.textContent || e.tagName).trim().slice(0, 20), Top: top, Left: left, Region: region, Sash: !!sash}; })()`))
			}
			for i := 1; i < len(stops); i++ {
				a, b := stops[i-1], stops[i]
				if b.Region == 0 || b.Region == 99 {
					break // round the page and back to the start
				}
				if b.Region < a.Region || b.Region == 98 {
					t.Errorf("Tab goes from %q (region %d) back to %q (region %d)", a.Name, a.Region, b.Name, b.Region)
					continue
				}
				if b.Region != a.Region || a.Sash || b.Sash {
					continue // a sash is a pane's whole edge: only its region counts
				}
				sameRow := b.Top < a.Top+4 && b.Top > a.Top-4
				if b.Top < a.Top-4 || (sameRow && b.Left < a.Left-1) {
					t.Errorf("Tab goes back up: %q (%.0f,%.0f) then %q (%.0f,%.0f)", a.Name, a.Left, a.Top, b.Name, b.Left, b.Top)
				}
			}
			Eval[bool](p, `(document.querySelector(".skip").focus(), true)`)
			p.Key("\r")
			p.WaitJS(`document.activeElement === document.getElementById("main")`)
		})
	}
}

// axe on the views with content and on the other sheets and bars: a
// chat with messages, git changes, a file, the row menu, the paste
// sheet, the offline bar; in English and in Persian (right to left).
func TestAxeViewsWithContent(t *testing.T) {
	src := axeSource(t)
	for _, vp := range Both {
		for _, tc := range []struct{ lang, theme string }{{"en-US", "dark"}, {"en-US", "light"}, {"fa-IR", "dark"}, {"fa-IR", "light"}} {
			lang := tc.lang
			t.Run(vp.Name+"/"+lang+"/"+tc.theme, func(t *testing.T) {
				s := StartServer(t)
				p := NewPageLang(t, s, vp, lang)
				p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": tc.theme})
				c := p.Login("c-1")
				c.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "fix the build"), aiMsg(1, md("doc", mdPara("Done: go test ./... passes."))))))
				p.WaitJS(`document.querySelector('[data-view="chat"] .msg')`)
				p.axe(src, "chat with messages")

				p.GoTab("git")
				waitGitSub(t, p, c)
				c.Send(t, Snap("git", 1, gitSnap()))
				p.WaitJS(`document.querySelector('.git-row-wrap[data-path="b.go"], .tr[data-path="b.go"]')`)
				p.axe(src, "git with changes")

				p.GoTab("term")
				c.WaitSub(t, "terms")
				showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a b c"), termItem(3, "zsh", false, true))
				if vp.Mobile {
					p.press(`document.querySelector('[data-view="term"] .trow')`, `document.querySelector(".row-menu")`)
				} else {
					Eval[bool](p, `(document.querySelector('[data-view="term"] .trow').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true})), true)`)
				}
				p.WaitJS(`document.querySelector(".row-menu")`)
				p.axe(src, "row menu")
				p.Key(kb.Escape)
				p.WaitJS(`!document.querySelector(".row-menu")`)
				if vp.Mobile {
					p.TapJS(`document.querySelector('[data-view="term"] .key-paste')`)
				} else { // the desktop's row menu
					Eval[bool](p, `(document.querySelector('[data-view="term"] .trow').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true})), true)`)
					p.TapJS(`[...document.querySelectorAll(".row-menu-item")].find((b) => b.textContent.endsWith("…"))`) // Paste…
				}
				p.WaitJS(`document.querySelector(".paste")`)
				p.axe(src, "paste sheet")
				p.Key(kb.Escape)

				s.Hub.Refuse(true)
				c.Drop()
				p.WaitJS(`document.querySelector(".bar-lost")`)
				p.axe(src, "offline")
			})
		}
	}
}

// axe on every desktop side bar view and editor page with data (the shot
// hub's): search results, source control, tasks and runs, databases and
// a table's grid, containers, MCP, settings, this device; dark and light.
// (Dense lists: see axeDense.)
func TestAxeDeskPanels(t *testing.T) {
	src := axeSource(t)
	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Desktop)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": theme})
			c := p.Login("c-axe")
			shotHub(t, c)
			has := func(text string) string { return `document.querySelector(".wb").innerText.includes(` + jsq(text) + `)` }
			visit := func(name, hash, ready string) {
				Eval[bool](p, `(location.hash = `+jsq(hash)+`, true)`)
				p.WaitJS(ready)
				p.axeDense(src, name)
			}
			Eval[bool](p, `(location.hash = "#/more/search", true)`)
			p.WaitJS(`document.querySelector(".wb-side input")`)
			Eval[bool](p, `(() => { const i = document.querySelector(".wb-side input"); i.focus(); i.value = "lineFilter"; i.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`)
			p.Key("\r")
			p.WaitJS(has("cover.go"))
			p.axeDense(src, "search")
			visit("source control", "#/git", `document.querySelector('.tr[data-group="changes"]')`)
			visit("tasks and runs", "#/more/tasks", has("fix the toast flicker"))
			visit("databases", "#/more/db", has("shop (local)"))
			p.TapJS(`document.querySelector('.wb-side .tr[data-conn="local"]')`)
			p.WaitJS(has("customers"))
			p.TapJS(`document.querySelector('.wb-side .tr[data-table="customers"]')`)
			p.WaitJS(has("ann@example.com"))
			p.axeDense(src, "table grid")
			visit("containers", "#/more/containers", has("shop-api-1"))
			visit("mcp", "#/more/mcp", has("claude-desktop"))
			visit("settings", "#/more/settings", `document.querySelector(".set-item")`)
			visit("this device", "#/more", `document.querySelector('.set-item[data-key="theme"]')`)
		})
	}
}
