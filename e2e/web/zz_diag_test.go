//go:build browser

package web

import (
	"testing"
	"time"
)

// Diagnostics for the macOS runner only (DO NOT MERGE): they log, never fail.

const diagGeo = `JSON.stringify({ua: navigator.userAgent.slice(-40), inner: [innerWidth, innerHeight], outer: [outerWidth, outerHeight], scroll: [scrollX, scrollY], vv: visualViewport && [visualViewport.offsetLeft, visualViewport.offsetTop, visualViewport.width, visualViewport.height, visualViewport.scale], doc: [document.documentElement.scrollWidth, document.documentElement.scrollHeight], body: [document.body.scrollTop, document.body.scrollHeight], dpr: devicePixelRatio, screen: [screen.width, screen.height]})`

const diagTouch = `(() => {
	window.__ev = [];
	const d = (e) => { const t = e.target; return ((t && t.className && String(t.className)) || (t && t.nodeName) || "?").slice(0, 30); };
	for (const n of ["pointerdown", "touchstart", "mousedown", "click"]) {
		document.addEventListener(n, (e) => { const p = e.touches ? e.touches[0] : e; window.__ev.push(n + "@" + d(e) + (p ? " client=" + Math.round(p.clientX) + "," + Math.round(p.clientY) + " page=" + Math.round(p.pageX) + "," + Math.round(p.pageY) + " screen=" + Math.round(p.screenX) + "," + Math.round(p.screenY) : "")); }, true);
	}
	return true;
})()`

func diagLog(t *testing.T, p *Page, what string) {
	t.Helper()
	t.Log(what, Eval[string](p, diagGeo))
	for _, e := range Eval[[]string](p, `window.__ev || []`) {
		t.Log("  ", e)
	}
}

func TestZZDiagTactileTapGeo(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	diagLog(t, p, "before focus:")
	Eval[bool](p, diagTouch)
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden`)
	time.Sleep(500 * time.Millisecond)
	diagLog(t, p, "dock up:")
	Eval[bool](p, diagTouch)
	t.Log("key L rect:", Eval[string](p, `JSON.stringify(`+tkKey("L")+`.getBoundingClientRect())`))
	p.TapJS(tkKey("L"))
	time.Sleep(500 * time.Millisecond)
	diagLog(t, p, "after key:")
}

func TestZZDiagRadialTapGeo(t *testing.T) {
	p, c := openRadial(t, Phone)
	p.GoTab("git")
	c.WaitSub(t, "git")
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('.gp-row[data-path="b.go"]')`)
	gitViews(t, Phone, c, noViews)
	Eval[bool](p, diagTouch)
	t.Log("fab rect:", Eval[string](p, `JSON.stringify(document.querySelector(".rm-fab").getBoundingClientRect())`))
	p.Tap(".rm-fab")
	time.Sleep(700 * time.Millisecond)
	diagLog(t, p, "after fab:")
	t.Log("overlay:", Eval[bool](p, `!!document.querySelector(".rm-overlay .rm-stage")`))
}
