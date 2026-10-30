//go:build browser && webdev

package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The card safety pack (WEB-UX-3, web UX P1, rule AH-1), on the
// component kit's real card dialogs (the cards view comes with WEB-15):
// for every kind, at 390 px with touch and at 1280 px with a mouse.

type cardKind struct {
	kind  string
	safe  string // first button
	risky string // "" for trust: no answer from the web at all
	said  string // the toast after the safe answer
	done  string // the toast after the risky answer
}

var cardKinds = []cardKind{
	{"tool", "Deny", "Allow once", "Denied.", "Allowed once. Running on the laptop."},
	{"review", "Reject", "Accept", "Rejected. Nothing changed.", "Accepted. The file changed on the laptop."},
	{"plan", "Deny", "Approve", "Denied.", "Allowed once. Running on the laptop."},
	{"mcp", "Deny", "Allow once", "Denied.", "Allowed once. Running on the laptop."},
	{"trust", "Later", "", "Later", ""},
}

const (
	dialog   = `document.querySelector('.sheet .card[role="dialog"]')`
	riskyBtn = `document.querySelector('.sheet .card .btn-ai')`
	toastsJS = `[...document.querySelectorAll(".toasts .toast-title")].map((e) => e.textContent).join("|")`
)

func openKit(t *testing.T, vp Viewport) *Page {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	// No sheet rise: the tap must land on the button where it will stay,
	// inside the first 600 ms. The arming runs the same (as ticks).
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	p.Do(chromedp.Navigate(s.Base + "/dev/kit"))
	p.WaitJS(ButtonByText("Open a tool card"))
	return p
}

// openCard opens a fresh dialog card of kind k and waits until it has
// taken the focus.
func openCard(p *Page, k string) {
	p.T.Helper()
	p.WaitJS("!" + dialog)
	p.TapJS(ButtonByText("Open a " + k + " card"))
	p.WaitJS(dialog + ` && ` + dialog + `.contains(document.activeElement)`)
}

func toasts(p *Page) string { return Eval[string](p, toastsJS) }

func forEachCard(t *testing.T, f func(t *testing.T, p *Page, k cardKind)) {
	for _, vp := range Both {
		for _, k := range cardKinds {
			t.Run(vp.Name+"/"+k.kind, func(t *testing.T) {
				f(t, openKit(t, vp), k)
			})
		}
	}
}

// The safe answer is the first button and has the focus.
func TestCardSafeChoiceFirstAndFocused(t *testing.T) {
	forEachCard(t, func(t *testing.T, p *Page, k cardKind) {
		openCard(p, k.kind)
		got := Eval[[]string](p, `[...document.querySelectorAll(".sheet .card-foot button")].map((b) => b.textContent.trim())`)
		if len(got) == 0 || got[0] != k.safe {
			t.Fatalf("buttons %q, want %q first", got, k.safe)
		}
		p.WaitJS(`document.activeElement === document.querySelector(".sheet .card-foot button")`)
		if k.risky != "" && !strings.Contains(strings.Join(got, "|"), k.risky) {
			t.Errorf("buttons %q have no %q", got, k.risky)
		}
	})
}

// Enter presses the focused button, which is the safe one.
func TestCardEnterIsTheSafeAnswer(t *testing.T) {
	forEachCard(t, func(t *testing.T, p *Page, k cardKind) {
		openCard(p, k.kind)
		p.WaitJS(`document.activeElement === document.querySelector(".sheet .card-foot button")`)
		p.Holds("true", 700*time.Millisecond) // armed by now: Enter must still not allow
		p.Key(kb.Enter)
		p.WaitJS("!" + dialog)
		if got := toasts(p); !strings.Contains(got, k.said) || (k.done != "" && strings.Contains(got, k.done)) {
			t.Errorf("after Enter the toasts say %q, want %q only", got, k.said)
		}
	})
}

// Esc means Later: the card closes and nothing is answered.
func TestCardEscIsLater(t *testing.T) {
	forEachCard(t, func(t *testing.T, p *Page, k cardKind) {
		openCard(p, k.kind)
		p.Holds("true", 700*time.Millisecond)
		p.Key(kb.Escape)
		p.WaitJS("!" + dialog)
		if got := toasts(p); got != "Later" {
			t.Errorf("after Esc the toasts say %q, want Later only", got)
		}
	})
}

// recordPress notes, at the moment a press begins, whether it is on the
// risky button and whether that button was armed: a slow machine may arm
// it before the test's tap lands, and then the early-tap check cannot be
// made.
func recordPress(btn string) string {
	return `(() => {
	window.__press = null;
	const b = ` + btn + `;
	const note = (e) => {
		if (e.type === "keydown" && e.key !== "Enter") return; // Tab moves focus, it presses nothing
		if (window.__press === null) window.__press = {hit: b.contains(e.target), armed: b.classList.contains("is-armed")};
	};
	window.__pressOff?.(); // the listeners of an earlier try
	for (const ev of ["pointerdown", "keydown"]) document.addEventListener(ev, note, {capture: true});
	window.__pressOff = () => { for (const ev of ["pointerdown", "keydown"]) document.removeEventListener(ev, note, {capture: true}); };
	return true;
})()`
}

type press struct{ Hit, Armed bool }

// pressedEarly reads what recordPress saw; a press that missed the
// button fails the test.
func pressedEarly(p *Page) bool {
	p.T.Helper()
	got := Eval[*press](p, `window.__press && {Hit: window.__press.hit, Armed: window.__press.armed}`)
	if got == nil {
		p.T.Fatal("no press reached the page")
	}
	if !got.Hit {
		p.T.Fatal("the press missed the risky button")
	}
	return !got.Armed
}

// earlyPress tries up to three fresh cards to press the risky button
// before it is armed; press does the pressing. It reports whether a
// press landed early (then nothing may have been answered).
func earlyPress(p *Page, k cardKind, press func()) bool {
	p.T.Helper()
	for try := 0; try < 3; try++ {
		openCard(p, k.kind)
		Eval[bool](p, recordPress(riskyBtn))
		press()
		if pressedEarly(p) {
			return true
		}
		p.Key(kb.Escape) // too slow this time: close it and try again
		p.WaitJS("!" + dialog)
	}
	return false
}

// A press that begins in the first 600 ms does nothing; once the fill
// is done the same press answers.
func TestCardRiskyIgnoredBeforeArming(t *testing.T) {
	forEachCard(t, func(t *testing.T, p *Page, k cardKind) {
		if k.risky == "" { // trust: the web cannot answer it at all
			openCard(p, k.kind)
			if Eval[bool](p, riskyBtn+` !== null`) {
				t.Fatal("a trust card has an answer button")
			}
			return
		}
		if !earlyPress(p, k, func() { p.TapJS(riskyBtn) }) {
			t.Skip("this machine never got a tap in within 600 ms")
		}
		p.Holds(dialog+` && !(`+toastsJS+`).includes(`+jsq(k.done)+`)`, 800*time.Millisecond)
		p.WaitJS(riskyBtn + `.classList.contains("is-armed")`)
		p.TapJS(riskyBtn)
		p.WaitJS(`(` + toastsJS + `).includes(` + jsq(k.done) + `)`)
	})
}

// Tab then Enter on the risky button before it is armed does nothing.
func TestCardEnterOnRiskyBeforeArmingDoesNothing(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p := openKit(t, vp)
			k := cardKinds[0]
			if !earlyPress(p, k, func() {
				p.Key(kb.Tab)
				p.Key(kb.Enter)
			}) {
				t.Skip("this machine never got a key in within 600 ms")
			}
			p.Holds(dialog+` && !(`+toastsJS+`).includes(`+jsq(k.done)+`)`, 800*time.Millisecond)
		})
	}
}

// A card that leaves the screen disarms; coming back it waits 600 ms
// again before a press counts.
func TestCardRearmsWhenBackInView(t *testing.T) {
	const inline = `document.querySelector('section[aria-labelledby="card-tool-o-t"] .btn-ai')`
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p := openKit(t, vp)
			p.WaitJS(inline)
			Eval[bool](p, `(`+inline+`.scrollIntoView({block: "center"}), true)`)
			p.WaitJS(inline + `.classList.contains("is-armed")`)
			Eval[bool](p, `(document.querySelector("#connect").scrollIntoView(), true)`)
			p.WaitJS(`!` + inline + `.classList.contains("is-armed")`)
			ok := false
			for try := 0; try < 3 && !ok; try++ {
				Eval[bool](p, recordPress(inline))
				p.TapJS(inline) // scrolls it back and taps at once
				ok = pressedEarly(p)
				if !ok {
					Eval[bool](p, `(document.querySelector("#connect").scrollIntoView(), true)`)
					p.WaitJS(`!` + inline + `.classList.contains("is-armed")`)
				}
			}
			if !ok {
				t.Skip("this machine never got a tap in within 600 ms")
			}
			p.Holds(`!(`+toastsJS+`).includes("Allowed once")`, 800*time.Millisecond)
			p.WaitJS(inline + `.classList.contains("is-armed")`)
			p.TapJS(inline)
			p.WaitJS(`(` + toastsJS + `).includes("Allowed once")`)
		})
	}
}

// No card, in any state, offers "Always allow"; an answered, offline or
// view-only card has no answer button at all.
func TestCardNoAlwaysAllowAndNoAnswerWhenNotOpen(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p := openKit(t, vp)
			for _, k := range cardKinds {
				openCard(p, k.kind)
				if txt := Eval[string](p, `[...document.querySelectorAll(".card")].map((c) => c.innerText).join("\n")`); strings.Contains(strings.ToLower(txt), "always") {
					t.Errorf("%s: a card says always: %q", k.kind, txt)
				}
				p.Key(kb.Escape)
				p.WaitJS("!" + dialog)
			}
			for _, st := range []string{"answered", "offline", "gone", "viewonly"} {
				sel := fmt.Sprintf(`document.querySelector('section[aria-labelledby="card-tool-%s-t"]')`, st)
				p.WaitJS(sel)
				if Eval[bool](p, sel+`.querySelector(".btn-ai") !== null`) {
					t.Errorf("a %s card still has an answer button", st)
				}
			}
		})
	}
}

func jsq(s string) string { return fmt.Sprintf("%q", s) }
