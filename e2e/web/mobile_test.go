//go:build browser

package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
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

// pinch moves two fingers on the element from gap0 to gap1 px apart.
func (p *Page) pinch(el string, gap0, gap1 float64) {
	p.T.Helper()
	c := Eval[struct{ X, Y float64 }](p, `(() => { const r = (`+el+`).getBoundingClientRect(); return {X: r.left + r.width / 2, Y: r.top + Math.min(r.height / 2, 120)}; })()`)
	id0, id1 := 1.0, 2.0
	pts := func(gap float64) []*input.TouchPoint {
		return []*input.TouchPoint{{X: c.X - gap/2, Y: c.Y, ID: &id0}, {X: c.X + gap/2, Y: c.Y, ID: &id1}}
	}
	touch := func(kind input.DispatchTouchEventType, tp []*input.TouchPoint) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: kind, TouchPoints: tp})
			return err
		})
	}
	acts := []chromedp.Action[chromedp.Void]{touch(input.DispatchTouchEventTypeTouchStart, pts(gap0))}
	for i := 1; i <= 8; i++ {
		acts = append(acts, touch(input.DispatchTouchEventTypeTouchMove, pts(gap0+(gap1-gap0)*float64(i)/8)))
	}
	acts = append(acts, touch(input.DispatchTouchEventTypeTouchEnd, []*input.TouchPoint{}))
	p.Do(acts...)
}

const rowFontJS = `parseFloat(getComputedStyle(document.querySelector(%q)).fontSize)`

// Two fingers on the terminal's rows or a file's rows change their text
// size (not the page's zoom); the size stays on this device, within
// 10 to 24 px, and the same size shows in both views.
func TestPinchChangesRowFont(t *testing.T) {
	p, c := openShell(t, Phone)
	row := `[data-view="term"] .trow`
	before := Eval[float64](p, fmt.Sprintf(rowFontJS, row))
	p.pinch(`document.querySelector('[data-view="term"] .term-rows')`, 60, 180)
	p.WaitJS(fmt.Sprintf(rowFontJS, row) + fmt.Sprintf(" > %f + 4", before))
	if z := Eval[float64](p, `visualViewport.scale`); z != 1 {
		t.Errorf("the page zoomed to %v; only the rows should change", z)
	}
	big := Eval[float64](p, fmt.Sprintf(rowFontJS, row))
	if big > 24 {
		t.Errorf("row font %v px, more than 24", big)
	}
	if !Eval[bool](p, `JSON.parse(localStorage.getItem("termocode.prefs.v1")).rowPx === `+fmt.Sprint(big)) {
		t.Error("the size is not kept in this device's prefs")
	}
	p.pinch(`document.querySelector('[data-view="term"] .term-rows')`, 300, 20)
	p.WaitJS(fmt.Sprintf(rowFontJS, row) + " === 10")

	Eval[bool](p, `(location.hash = "#/files/a.go", true)`)
	var r *webproto.Req
	for r == nil {
		if m, ok := c.Next(t).(*webproto.Req); ok && m.Op == "buf.open" {
			r = m
		}
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": 2, "path": "a.go", "tick": 3, "lines": 2, "dirty": false, "ft": "go", "readonly": false})})
	c.WaitSub(t, "buf:2")
	c.Send(t, Snap("buf:2", 1, bufSnap(2, "a.go", "package main", "func main() {}")))
	p.WaitJS(filesView + `.querySelector(".crow")`)
	if got := Eval[float64](p, fmt.Sprintf(rowFontJS, `[data-view="files"] .crow`)); got != 10 {
		t.Errorf("file rows are %v px; want the terminal's 10", got)
	}
	p.pinch(filesView+`.querySelector(".lines")`, 50, 100)
	p.WaitJS(fmt.Sprintf(rowFontJS, `[data-view="files"] .crow`) + " > 12")
}

// The pinch has a button twin (WCAG 2.5.7) in the terminal's ⓘ sheet:
// smaller, bigger and the default size, on the phone and the desktop.
func TestRowFontButtons(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openShell(t, vp)
			row := `[data-view="term"] .trow`
			def := Eval[float64](p, fmt.Sprintf(rowFontJS, row))
			p.Tap(`[data-view="term"] .term-info-btn`)
			p.Tap(`.font-size .font-bigger`)
			p.WaitJS(fmt.Sprintf(rowFontJS, row) + fmt.Sprintf(" === %f + 1", def))
			if got := p.Text(`.font-size .font-now`); got != fmt.Sprintf("%.0f px", def+1) {
				t.Errorf("size label %q", got)
			}
			p.Tap(`.font-size .font-smaller`)
			p.Tap(`.font-size .font-smaller`)
			p.WaitJS(fmt.Sprintf(rowFontJS, row) + fmt.Sprintf(" === %f - 1", def))
			p.Tap(`.font-size .font-reset`)
			p.WaitJS(fmt.Sprintf(rowFontJS, row) + fmt.Sprintf(" === %f", def))
			if !Eval[bool](p, `JSON.parse(localStorage.getItem("termocode.prefs.v1")).rowPx === 0`) {
				t.Error("reset did not go back to the default size")
			}
		})
	}
}

// stubCopy makes the page's clipboard writes land in window.__copied
// (the test cannot read the real clipboard).
func (p *Page) stubCopy() {
	p.T.Helper()
	Eval[bool](p, `(navigator.clipboard.writeText = (s) => { window.__copied = s; return Promise.resolve(); }, true)`)
}

// press holds a finger on the element until cond is true, then lifts it.
func (p *Page) press(el, cond string) {
	p.T.Helper()
	pt := Eval[struct{ X, Y float64 }](p, `(() => { const r = (`+el+`).getBoundingClientRect(); return {X: r.left + 8, Y: r.top + r.height / 2}; })()`)
	id := 1.0
	touch := func(kind input.DispatchTouchEventType, tp []*input.TouchPoint) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: kind, TouchPoints: tp})
			return err
		})
	}
	p.Do(touch(input.DispatchTouchEventTypeTouchStart, []*input.TouchPoint{{X: pt.X, Y: pt.Y, ID: &id}}))
	p.WaitJS(cond)
	p.Do(touch(input.DispatchTouchEventTypeTouchEnd, []*input.TouchPoint{}))
}

const rowMenu = `document.querySelector(".row-menu")`

// A long press on a terminal row (a right click on the desktop) opens a
// menu: copy the row, copy all rows, paste. A tap does not open it, and
// Esc closes it.
func TestTermRowMenu(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openShell(t, vp)
			p.stubCopy()
			row := `[...document.querySelectorAll('[data-view="term"] .trow')].find((r) => r.textContent === "a b c")`
			p.TapJS(row)
			p.Holds(`!`+rowMenu, 600*time.Millisecond)
			open := func() {
				if vp.Mobile {
					p.press(row, rowMenu)
				} else {
					pt := Eval[struct{ X, Y float64 }](p, `(() => { const r = (`+row+`).getBoundingClientRect(); return {X: r.left + 8, Y: r.top + r.height / 2}; })()`)
					p.Do(chromedp.MouseClickXY(pt.X, pt.Y, chromedp.ButtonRight))
					p.WaitJS(rowMenu)
				}
			}
			open()
			p.WaitJS(rowMenu + `.contains(document.activeElement)`) // focus moves into the menu
			p.TapJS(ButtonByText("Copy this row"))
			p.WaitJS(`window.__copied === "a b c" && !` + rowMenu)
			open()
			p.TapJS(ButtonByText("Copy all rows"))
			p.WaitJS(`window.__copied === "$ ls\na b c"`)
			open()
			p.WaitJS(rowMenu + `.contains(document.activeElement)`)
			p.Key(kb.Escape)
			p.WaitJS(`!` + rowMenu)
			open()
			p.TapJS(ButtonByText("Paste…"))
			p.WaitJS(`document.querySelector(".paste") && !` + rowMenu)
		})
	}
}

// onNewDocument runs js in the page before its own scripts, on every load.
func (p *Page) onNewDocument(js string) {
	p.T.Helper()
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: js})
		return err
	}))
}

// fakeWakeLock records the page's wake lock calls in window.__locks and
// keeps the number held in window.__held.
const fakeWakeLock = `(() => {
	window.__locks = []; window.__held = 0;
	Object.defineProperty(navigator, "wakeLock", {configurable: true, value: {
		request: async (type) => {
			window.__locks.push("request " + type); window.__held++;
			const s = {released: false, type, addEventListener() {}, removeEventListener() {},
				release: async () => { if (!s.released) { s.released = true; window.__held--; window.__locks.push("release"); } }};
			return s;
		},
	}});
})()`

// setHidden makes the page think it went to the background or came back.
func (p *Page) setHidden(hidden bool) {
	p.T.Helper()
	Eval[bool](p, fmt.Sprintf(`(Object.defineProperty(document, "hidden", {configurable: true, get: () => %t}),
		Object.defineProperty(document, "visibilityState", {configurable: true, get: () => %q}),
		document.dispatchEvent(new Event("visibilitychange")), true)`, hidden, map[bool]string{true: "hidden", false: "visible"}[hidden]))
}

// "Keep screen on" in the terminal's ⓘ sheet is off until the user turns
// it on; then the screen stays on while the shell on screen runs
// something, and is let go when it stops, when the page hides or when the
// user turns it off.
func TestWakeLockWhileARunStreams(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.onNewDocument(fakeWakeLock)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ go test ./..."), termItem(3, "zsh", true, true))
	p.Holds(`window.__held === 0`, 300*time.Millisecond)

	p.Tap(`[data-view="term"] .term-info-btn`)
	p.TapJS(`[...document.querySelectorAll(".term-about .chip")].find((b) => b.textContent.includes("Keep screen on"))`)
	p.WaitJS(`window.__held === 1 && window.__locks[0] === "request screen"`)
	if !Eval[bool](p, `JSON.parse(localStorage.getItem("termocode.prefs.v1")).wakeLock === true`) {
		t.Error("the choice is not kept on this device")
	}

	c.Send(t, Snap("terms", 2, termsSnap(termItem(3, "zsh", false, true))))
	p.WaitJS(`window.__held === 0`)
	c.Send(t, Snap("terms", 3, termsSnap(termItem(3, "zsh", true, true))))
	p.WaitJS(`window.__held === 1`)

	p.setHidden(true)
	p.WaitJS(`window.__held === 0`)
	p.setHidden(false)
	p.WaitJS(`window.__held === 1`)

	p.TapJS(`[...document.querySelectorAll(".term-about .chip")].find((b) => b.textContent.includes("Keep screen on"))`)
	p.WaitJS(`window.__held === 0`)
}
