//go:build browser

package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// The phone's terminal (PHONE-TERM, owner 2026-11-15: "emulate the
// terminal exactly"): the terminal itself is the input, no command box;
// it fills the screen; a slim strip of shells; the key bar right above
// the keyboard; the tab bar hides while the keyboard is up.

// xtermInput is the terminal engine's input of the phone's terminal.
const xtermInput = `document.querySelector('[data-view="term"] .pterm-x textarea')`

// xtermRows are the rows the terminal engine shows, trailing blanks cut.
func xtermRows(p *Page) []string {
	got := Eval[[]string](p, `[...document.querySelectorAll('[data-view="term"] .xterm-rows > div')].map((d) => d.textContent.replace(/[\s ]+$/, ""))`)
	for len(got) > 0 && got[len(got)-1] == "" {
		got = got[:len(got)-1]
	}
	return got
}

// pkey presses one key as a browser gets it from a keyboard: the key
// down, its character (text, if any) and the key up. A key down the page
// takes (preventDefault) types no character, as on a real keyboard.
func pkey(p *Page, key, code string, vk int64, text string) {
	p.T.Helper()
	ev := func(prm input.DispatchKeyEventParams) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, input.DispatchKeyEvent, prm)
			return err
		})
	}
	base := input.DispatchKeyEventParams{Key: key, Code: code, WindowsVirtualKeyCode: vk, NativeVirtualKeyCode: vk}
	down, up := base, base
	down.Type, up.Type = input.DispatchKeyEventTypeRawKeyDown, input.DispatchKeyEventTypeKeyUp
	acts := []chromedp.Action[chromedp.Void]{ev(down)}
	if text != "" {
		ch := base
		ch.Type, ch.Text, ch.UnmodifiedText = input.DispatchKeyEventTypeChar, text, text
		acts = append(acts, ev(ch))
	}
	p.Do(append(acts, ev(up))...)
}

// swipe drags a finger over the element's middle by dy px (down > 0:
// the content moves down, older rows come into view).
func swipe(p *Page, el string, dy float64) {
	p.T.Helper()
	pt := Eval[struct{ X, Y float64 }](p, `(() => { const r = (`+el+`).getBoundingClientRect(); return {X: r.left + r.width / 2, Y: r.top + r.height / 3}; })()`)
	id := 1.0
	touch := func(kind input.DispatchTouchEventType, tp []*input.TouchPoint) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: kind, TouchPoints: tp})
			return err
		})
	}
	acts := []chromedp.Action[chromedp.Void]{touch(input.DispatchTouchEventTypeTouchStart, []*input.TouchPoint{{X: pt.X, Y: pt.Y, ID: &id}})}
	for k := 1.0; k <= 10; k++ {
		acts = append(acts, touch(input.DispatchTouchEventTypeTouchMove, []*input.TouchPoint{{X: pt.X, Y: pt.Y + dy*k/10, ID: &id}}))
	}
	p.Do(append(acts, touch(input.DispatchTouchEventTypeTouchEnd, []*input.TouchPoint{}))...)
}

// typeLetters types lower-case letters with pkey.
func typeLetters(p *Page, s string) {
	p.T.Helper()
	for _, r := range s {
		u := strings.ToUpper(string(r))
		pkey(p, string(r), "Key"+u, int64(u[0]), string(r))
	}
}

// tapAbout opens About this terminal: on the phone from the strip's ⋯
// menu, on the desktop from ⓘ in the panel title.
func tapAbout(p *Page) {
	p.T.Helper()
	if Eval[bool](p, `!!document.querySelector('[data-view="term"] .pterm-more')`) {
		p.TapJS(`document.querySelector('[data-view="term"] .pterm-more')`)
		p.WaitJS(rowMenu)
		pickItem(p, "About this terminal")
		return
	}
	p.TapJS(`document.querySelector('[data-view="term"] .term-info-btn, .wb-panel .term-info-btn')`)
}

func termShot(t *testing.T, p *Page, name string) {
	t.Helper()
	if dir := os.Getenv(shotsEnv); dir != "" {
		snap(t, p, filepath.Join(dir, "phone-term-"+name+".png"), `document.querySelector('[data-view="term"]')`)
	}
}

// One shell on screen: no header row, no command box, no Send; the
// shells are one slim strip; the terminal fills the space down to the
// key bar and shows the laptop's rows.
func TestPhoneTermFillsTheScreen(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a.go  b.go", "$ "), termItem(3, "zsh", false, true), termItem(9, "build", true, false))
	termShot(t, p, "after")
	if got := strings.Join(xtermRows(p), "|"); got != "$ ls|a.go  b.go|$" {
		t.Errorf("terminal rows %q", got)
	}
	for _, sel := range []string{".term-line", ".view-head", ".send", "input:not(.pterm-ask input)"} {
		if Eval[bool](p, termView+`.querySelector(`+jsq(sel)+`) !== null`) {
			t.Errorf("the terminal still has %s", sel)
		}
	}
	if h := Eval[float64](p, termView+`.querySelector(".pterm-strip").getBoundingClientRect().height`); h > 46 {
		t.Errorf("the shells strip is %.0f px tall, want one slim line", h)
	}
	if !Eval[bool](p, `!!(`+termView+`.querySelector('.pterm-strip .term-chip[data-buf="9"]') && `+termView+`.querySelector(".pterm-strip .pterm-more"))`) {
		t.Error("the strip has not every shell and ⓘ")
	}
	gap := Eval[float64](p, `(() => { const x = `+termView+`.querySelector(".pterm-x").getBoundingClientRect(), k = (`+termView+`.querySelector(".pterm-ai") ?? `+termView+`.querySelector(".keybar")).getBoundingClientRect(); return k.top - x.bottom; })()`)
	if gap > 24 || gap < -1 {
		t.Errorf("%.0f px between the terminal and the key bar, want the terminal to fill the space", gap)
	}
	got := Eval[[]string](p, `[...`+termView+`.querySelectorAll(".keybar .key")].slice(0, 9).map((k) => k.textContent)`)
	if strings.Join(got, " ") != "Esc Tab Ctrl Alt - ← ↓ ↑ →" {
		t.Errorf("key bar starts %q", got)
	}
}

// A tap on the terminal gives its input the focus (the phone keyboard
// opens) and every key goes straight to the shell: letters, Enter,
// Backspace, arrows. The input takes no autocorrect and no capitals.
// Ctrl from the key bar is for the next key, and a key bar tap keeps
// the focus in the terminal (the keyboard stays open).
func TestPhoneTermTypesStraightIntoTheShell(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(`document.activeElement === ` + xtermInput)
	for _, a := range [][2]string{{"autocapitalize", "off"}, {"autocorrect", "off"}, {"spellcheck", "false"}} {
		if got := Eval[string](p, xtermInput+`.getAttribute(`+jsq(a[0])+`)`); got != a[1] {
			t.Errorf("input %s=%q, want %q", a[0], got, a[1])
		}
	}
	typeLetters(p, "ls")
	if a, b := expectSend(t, c), expectSend(t, c); a+b != "ls" {
		t.Errorf("typing sent %q %q", a, b)
	}
	for _, k := range []struct {
		key, code  string
		vk         int64
		text, want string
	}{{"Enter", "Enter", 13, "\r", "\r"}, {"Backspace", "Backspace", 8, "", "\x7f"}, {"ArrowUp", "ArrowUp", 38, "", "\x1b[A"}} {
		pkey(p, k.key, k.code, k.vk, k.text)
		if got := expectSend(t, c); got != k.want {
			t.Errorf("key %s sent %q, want %q", k.key, got, k.want)
		}
	}
	tapKey(p, "Ctrl")
	if !Eval[bool](p, `document.activeElement === `+xtermInput) {
		t.Error("a key bar tap took the focus from the terminal")
	}
	typeLetters(p, "c")
	if got := expectSend(t, c); got != "\x03" {
		t.Errorf("Ctrl then c sent %q", got)
	}
	typeLetters(p, "x")
	if got := expectSend(t, c); got != "x" {
		t.Errorf("after Ctrl's one key, x sent %q", got)
	}
}

// The keyboard opens: the page fits the space above it, the tab bar
// hides, the key bar sits right above the keyboard and the terminal
// keeps the rest (fewer rows); it closes: all comes back.
func TestPhoneTermKeyboardRefit(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	rows0 := Eval[int](p, termView+`.querySelectorAll(".xterm-rows > div").length`)
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(`document.activeElement === ` + xtermInput)
	p.resize(Phone.W, 480)
	p.WaitJS(`document.documentElement.dataset.kb === "up" && getComputedStyle(document.querySelector(".tabbar")).display === "none"`)
	p.WaitJS(`(() => { const k = ` + termView + `.querySelector(".keybar").getBoundingClientRect(); return k.bottom <= visualViewport.height + 1 && k.bottom >= visualViewport.height - 24; })()`)
	p.WaitJS(termView + `.querySelectorAll(".xterm-rows > div").length < ` + fmt.Sprint(rows0))
	termShot(t, p, "after-keyboard")
	Eval[bool](p, xtermInput+`.blur() || true`)
	p.resize(Phone.W, Phone.H)
	p.WaitJS(`document.documentElement.dataset.kb !== "up" && getComputedStyle(document.querySelector(".tabbar")).display !== "none"`)
	p.WaitJS(termView + `.querySelectorAll(".xterm-rows > div").length === ` + fmt.Sprint(rows0))
}

// The way in for an on-screen keyboard of the page (home.js):
// pressTermKey sends one key with its modifiers, setTermInputMode sets
// the terminal input's inputmode.
func TestPhoneTermKeyEntryPoint(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.WaitJS(xtermInput)
	Eval[bool](p, `(import("/assets/home.js").then((m) => { window.__tk = m; }), true)`)
	p.WaitJS(`!!window.__tk`)
	for _, k := range []struct{ js, want string }{
		{`{key: "c", code: "KeyC", ctrl: true}`, "\x03"},
		{`{key: "ArrowLeft", alt: true}`, "\x1b[1;3D"},
		{`{key: "F5"}`, "\x1b[15~"},
		{`{key: "é"}`, "é"},
		{`{key: "Enter"}`, "\r"},
	} {
		if !Eval[bool](p, `window.__tk.pressTermKey(`+k.js+`)`) {
			t.Errorf("pressTermKey(%s) sent nothing", k.js)
			continue
		}
		if got := expectSend(t, c); got != k.want {
			t.Errorf("pressTermKey(%s) sent %q, want %q", k.js, got, k.want)
		}
	}
	Eval[bool](p, `(window.__tk.setTermInputMode("none"), true)`)
	p.WaitJS(xtermInput + `.getAttribute("inputmode") === "none"`)
	Eval[bool](p, `(window.__tk.setTermInputMode("text"), true)`)
	p.WaitJS(xtermInput + `.getAttribute("inputmode") === "text"`)
}
