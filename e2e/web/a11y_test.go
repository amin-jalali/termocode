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
func (p *Page) axe(src, where string) {
	p.T.Helper()
	if !Eval[bool](p, `typeof window.axe === "object"`) {
		Eval[bool](p, src+`; true`)
	}
	Eval[bool](p, `(window.__axe = undefined, axe.run(document, {
		runOnly: {type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]},
		resultTypes: ["violations"],
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
				p.Tap(`[data-view="term"] .term-info-btn`)
				p.WaitJS(`document.querySelector(".term-about")`)
				p.axe(src, "term about sheet")
				p.Key(kb.Escape)
				p.TapJS(`document.querySelector('[data-view="term"] .keybar .key-edit')`)
				p.WaitJS(`document.querySelector(".keys-edit")`)
				p.axe(src, "key bar editor")
				p.TapJS(ButtonByText("Done"))
				p.WaitJS(`!document.querySelector(".keys-edit")`)
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
