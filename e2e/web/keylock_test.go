//go:build browser

package web

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// WEBP-11: terminal keys win over the browser. A browser tab keeps
// Ctrl+W, Ctrl+T and Ctrl+N for itself; in full screen the Keyboard Lock
// API hands them to the page, and an installed app window has them too.

const keyHint = `document.querySelector('[data-view="term"] .dterm-keyhint')`

// stubKeyboard makes navigator.keyboard.lock and unlock record their calls.
func stubKeyboard(p *Page) {
	p.T.Helper()
	Eval[bool](p, `(navigator.keyboard.lock = (codes) => (window.__lock = codes === undefined ? "all" : codes.join(), Promise.resolve()),
		navigator.keyboard.unlock = () => (window.__unlock = true), true)`)
}

// Without full screen or the app, a one-time hint says which keys the
// browser keeps and how to get them; Got it hides it for good.
func TestDeskTermKeysHintOnce(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.WaitJS(keyHint)
	txt := Eval[string](p, keyHint+`.textContent`)
	for _, k := range []string{"Ctrl+W", "Ctrl+T", "Ctrl+N", "Full screen"} {
		if !strings.Contains(txt, k) {
			t.Errorf("the hint %q does not name %s", txt, k)
		}
	}
	if Eval[bool](p, keyHint+`.querySelector(".dterm-install") !== null`) {
		t.Error("Install as app is offered while the browser offers no install")
	}
	if !Eval[bool](p, `document.activeElement === document.querySelector('[data-view="term"] .dterm-pane.is-focus .dterm-input')`) {
		t.Error("the hint took the focus from the terminal")
	}
	p.TapJS(keyHint + `.querySelector(".dterm-keyhint-ok")`)
	p.WaitJS(`!` + keyHint)
	if !Eval[bool](p, `JSON.parse(localStorage.getItem("termocode.prefs.v1")).keysHintDone === true`) {
		t.Error("Got it is not kept for the next visit")
	}
}

// Full screen locks the keys (Keyboard Lock API): the hint goes, and
// Ctrl+W reaches the shell as ^W. Leaving full screen unlocks them.
func TestDeskTermFullScreenLocksKeys(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	stubKeyboard(p)
	p.Tap(".dterm-fullscreen")
	if runtime.GOOS == "darwin" {
		// The macOS runner's headless Chrome may refuse full screen even
		// to a real click ("TypeError: not granted", run 44197458253; the
		// owner's rule 2026-11-18): then the page must say so in a toast
		// and lock nothing. Granted, it goes on as everywhere else.
		const refused = `[...document.querySelectorAll(".toasts .toast-title")].some((e) => e.textContent === "The browser did not go full screen.")`
		p.WaitJS(`(window.__lock === "all" && document.fullscreenElement !== null) || ` + refused)
		if Eval[bool](p, refused) {
			if Eval[bool](p, `window.__lock !== undefined || document.fullscreenElement !== null`) {
				t.Error("full screen was refused, yet the keys were locked")
			}
			t.Log("this Chrome refused full screen; the page said so in a toast")
			return
		}
	}
	p.WaitJS(`window.__lock === "all" && document.fullscreenElement !== null`)
	p.WaitJS(`!` + keyHint)
	p.TapJS(`document.querySelector('[data-view="term"] .dterm-pane.is-focus .trow')`)
	p.WaitJS(`document.activeElement === document.querySelector('[data-view="term"] .dterm-pane.is-focus .dterm-input')`)
	p.Do(chromedp.KeyEvent("w", chromedp.KeyModifiers(chromedp.ModifierCtrl)))
	if _, s := sendTo(t, c); s != "\x17" {
		t.Errorf("Ctrl+W sent %q, want ^W", s)
	}
	Eval[bool](p, `(document.exitFullscreen(), true)`)
	p.WaitJS(`window.__unlock === true`)
	p.WaitJS(keyHint)
}

// The hint's Install as app shows once the browser offers the install,
// and asks the browser to install.
// A browser that refuses full screen gets a toast that says so, and no
// key is locked (on every system: the browser is stubbed to refuse).
func TestDeskTermFullScreenRefusedSaysSo(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	stubKeyboard(p)
	Eval[bool](p, `(document.documentElement.requestFullscreen = () => Promise.reject(new TypeError("not granted")), true)`)
	p.Tap(".dterm-fullscreen")
	p.WaitJS(`[...document.querySelectorAll(".toasts .toast-title")].some((e) => e.textContent === "The browser did not go full screen.")`)
	if Eval[bool](p, `window.__lock !== undefined || document.fullscreenElement !== null`) {
		t.Error("full screen was refused, yet the keys were locked")
	}
	p.WaitJS(keyHint) // the hint still offers the ways to get the keys
}

func TestDeskTermKeysHintInstallsApp(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.WaitJS(keyHint)
	Eval[bool](p, `(() => {
		const e = new Event("beforeinstallprompt", { cancelable: true });
		e.prompt = () => ((window.__prompted = true), Promise.resolve());
		dispatchEvent(e);
		return true;
	})()`)
	p.TapJS(keyHint + `.querySelector(".dterm-install")`)
	p.WaitJS(`window.__prompted === true`)
}

// In the installed app's window the keys are the page's already: no hint
// and no Full screen. (DevTools cannot emulate display-mode, so the page
// gets a matchMedia that answers standalone before it loads.)
func TestDeskTermKeysNoHintInTheApp(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: `{
			const mm = window.matchMedia.bind(window);
			window.matchMedia = (q) => {
				const m = mm(q);
				return q === "(display-mode: standalone)" ? { matches: true, media: q, onchange: null, addEventListener() {}, removeEventListener() {} } : m;
			};
		}`})
		return err
	}))
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.Holds(`!`+keyHint+` && document.querySelector('[data-view="term"] .trow')`, 300*time.Millisecond)
	if Eval[bool](p, `document.querySelector(".dterm-fullscreen") !== null`) {
		t.Error("the app window offers Full screen for the keys it has already")
	}
}
