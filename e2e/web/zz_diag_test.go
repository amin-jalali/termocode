//go:build browser

package web

import (
	"testing"
	"time"
)

// TestZZDiagTactileTap logs what a tap on a Tactile key does (diagnostic
// for the macOS runner; not for merging).
func TestZZDiagTactileTap(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden`)
	Eval[bool](p, `(() => {
		window.__ev = [];
		const d = (e) => { const t = e.target; return (t && t.className && String(t.className).slice(0, 40)) || (t && t.nodeName) || "?"; };
		for (const n of ["pointerdown", "pointerup", "pointercancel", "touchstart", "touchend", "touchcancel", "mousedown", "mouseup", "click", "focusin", "focusout", "gotpointercapture", "lostpointercapture"]) {
			document.addEventListener(n, (e) => window.__ev.push(n + "@" + d(e) + (e.pointerType ? "/" + e.pointerType : "") + (e.defaultPrevented ? "/prevented" : "") + " active=" + (document.activeElement && (document.activeElement.className || document.activeElement.nodeName)).toString().slice(0, 30)), true);
		}
		return true;
	})()`)
	t.Log("before:", Eval[string](p, `navigator.userAgent + " maxTouch=" + navigator.maxTouchPoints + " active=" + (document.activeElement && document.activeElement.className)`))
	p.TapJS(tkKey("L"))
	time.Sleep(700 * time.Millisecond)
	for _, e := range Eval[[]string](p, `window.__ev`) {
		t.Log(e)
	}
	t.Log("dock hidden:", Eval[bool](p, `!(`+tkDock+`) || `+tkDock+`.hidden`))
	select {
	case m := <-c.in:
		t.Logf("hub got %s", show(m))
	case <-time.After(2 * time.Second):
		t.Log("hub got nothing")
	}
}
