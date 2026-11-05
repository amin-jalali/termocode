//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

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

// In the installed app's window the keys are the page's already: no hint.
func TestDeskTermKeysNoHintInTheApp(t *testing.T) {
	p, c := openTerm(t, Desktop)
	p.Media(map[string]string{"display-mode": "standalone", "prefers-reduced-motion": "reduce"})
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.Holds(`!`+keyHint+` && document.querySelector('[data-view="term"] .trow')`, 300*time.Millisecond)
	if Eval[bool](p, `document.querySelector(".dterm-fullscreen") !== null`) {
		t.Error("the app window offers Full screen for the keys it has already")
	}
}
