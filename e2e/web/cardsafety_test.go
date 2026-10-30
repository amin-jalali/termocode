//go:build browser

package web

import "fmt"

// Shared by the card safety pack on the kit page (cards_test.go, webdev)
// and on the real cards view (cardsview_test.go).

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

func toasts(p *Page) string { return Eval[string](p, toastsJS) }

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

func jsq(s string) string { return fmt.Sprintf("%q", s) }
