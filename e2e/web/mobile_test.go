//go:build browser

package web

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
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

// PhoneLandscape is the design phone turned sideways.
var PhoneLandscape = Viewport{Name: "landscape", W: 844, H: 390, Scale: 3, Mobile: true}

// insets emulates env(safe-area-inset-*) in CSS pixels.
func (p *Page) insets(top, right, bottom, left int64) {
	p.T.Helper()
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, emulation.SetSafeAreaInsetsOverride, emulation.SetSafeAreaInsetsOverrideParams{
			Insets: &emulation.SafeAreaInsets{Top: &top, Right: &right, Bottom: &bottom, Left: &left},
		})
		return err
	}))
}

// outsideSafeJS lists the controls that show in a safe-area inset (under
// a notch, a rounded corner or the home bar). Only the part of a control
// its scrolling boxes show counts; a control scrolled out of sight does
// not.
func outsideSafeJS(top, right, bottom, left int) string {
	return fmt.Sprintf(`(() => {
		const W = document.documentElement.clientWidth, H = visualViewport.height;
		const shown = (el) => {
			const r0 = el.getBoundingClientRect();
			let r = {left: r0.left, right: r0.right, top: r0.top, bottom: r0.bottom};
			for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
				const cs = getComputedStyle(a);
				if (cs.overflowX === "visible" && cs.overflowY === "visible") continue;
				const c = a.getBoundingClientRect();
				r = {left: Math.max(r.left, c.left), right: Math.min(r.right, c.right), top: Math.max(r.top, c.top), bottom: Math.min(r.bottom, c.bottom)};
			}
			return r;
		};
		const out = [];
		for (const el of document.querySelectorAll("button, input, textarea, a[href], [role=tab], .strip-item, .brand")) {
			const r0 = el.getBoundingClientRect();
			if (r0.width === 0 || r0.height === 0 || getComputedStyle(el).visibility === "hidden") continue;
			const r = shown(el);
			if (r.right - r.left < 1 || r.bottom - r.top < 1 || r.bottom <= 0 || r.top >= H) continue;
			const name = (el.getAttribute("aria-label") || el.textContent || el.className).trim().slice(0, 24);
			if (r.left < %d - 0.5 || r.right > W - %d + 0.5 || r.bottom > H - %d + 0.5 || r.top < %d - 0.5) out.push(name + " " + Math.round(r.left) + ".." + Math.round(r.right) + " x " + Math.round(r.top) + ".." + Math.round(r.bottom));
		}
		return out.slice(0, 12);
	})()`, left, right, bottom, top)
}

// Turned sideways under a notch, no control sits in a safe-area inset,
// in left-to-right and right-to-left; upright, the top and bottom insets
// are kept the same way.
func TestSafeAreaInsets(t *testing.T) {
	for _, tc := range []struct {
		vp                       Viewport
		top, right, bottom, left int64
		rtl                      bool
	}{
		{PhoneLandscape, 0, 47, 21, 47, false},
		{PhoneLandscape, 0, 47, 21, 47, true},
		{Phone, 47, 0, 34, 0, false},
	} {
		name := tc.vp.Name
		if tc.rtl {
			name += "/rtl"
		}
		t.Run(name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, tc.vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			p.insets(tc.top, tc.right, tc.bottom, tc.left)
			c := p.Login("c-1")
			if tc.rtl {
				Eval[bool](p, `(document.documentElement.dir = "rtl", true)`)
			}
			for _, tab := range []string{"chat", "term", "more"} {
				p.GoTab(tab)
				if tab == "term" {
					c.Subs(t, "terms")
					showTerm(t, p, c, termSnap(3, 0, 80, "$ ls"), termItem(3, "zsh", false, true))
				}
				if bad := Eval[[]string](p, outsideSafeJS(int(tc.top), int(tc.right), int(tc.bottom), int(tc.left))); len(bad) > 0 {
					t.Errorf("%s: controls in the safe-area insets:\n  %s", tab, strings.Join(bad, "\n  "))
				}
			}
		})
	}
}
