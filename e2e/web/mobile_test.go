//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Mobile polish (WEB-36, web UX 6, 8.1).

// openShell opens the Term tab with one shell on screen.
func openShell(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	p, c := openTerm(t, vp)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a b c"), termItem(3, "zsh", false, true))
	return p, c
}

// resize changes the emulated screen, as the on-screen keyboard does
// when it opens or closes.
func (p *Page) resize(w, h int64) {
	p.T.Helper()
	view := []chromedp.EmulateViewportOption{chromedp.EmulateScale(p.VP.Scale)}
	if p.VP.Mobile {
		view = append(view, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	p.Do(chromedp.EmulateViewport(w, h, view...))
}

// When the on-screen keyboard opens over the terminal's input line, the
// page fits the space above it: the tab bar hides (focus not obscured,
// WCAG 2.4.11) and the input line stays on screen. When it closes, the
// tab bar comes back.
func TestVisualViewportKeyboardRefit(t *testing.T) {
	p, _ := openShell(t, Phone)
	if Eval[bool](p, `document.documentElement.dataset.kb === "up"`) {
		t.Fatal("keyboard flag before any input has focus")
	}
	Eval[bool](p, termLine+`.focus() || true`)
	p.resize(Phone.W, 480)
	p.WaitJS(`document.documentElement.dataset.kb === "up"`)
	p.WaitJS(`getComputedStyle(document.querySelector(".tabbar")).display === "none"`)
	p.WaitJS(`(() => { const r = ` + termLine + `.getBoundingClientRect(); return r.bottom <= visualViewport.height && r.top >= 0; })()`)
	if h := Eval[float64](p, `document.querySelector(".shell").getBoundingClientRect().height`); h > 481 {
		t.Errorf("the shell is %.0f px tall over a 480 px visible area", h)
	}
	Eval[bool](p, termLine+`.blur() || true`)
	p.resize(Phone.W, Phone.H)
	p.WaitJS(`document.documentElement.dataset.kb !== "up" && getComputedStyle(document.querySelector(".tabbar")).display !== "none"`)
	p.noOverflow("after the keyboard")
}

// A smaller window without a focused input is not a keyboard: the tab
// bar stays.
func TestVisualViewportShorterWindowIsNotAKeyboard(t *testing.T) {
	p, _ := openShell(t, Phone)
	p.resize(Phone.W, 480)
	p.WaitJS(`document.querySelector(".shell").getBoundingClientRect().height <= 481`)
	if Eval[bool](p, `document.documentElement.dataset.kb === "up"`) {
		t.Error("keyboard flag with no input focused")
	}
	if Eval[string](p, `getComputedStyle(document.querySelector(".tabbar")).display`) == "none" {
		t.Error("the tab bar hid without a keyboard")
	}
}
