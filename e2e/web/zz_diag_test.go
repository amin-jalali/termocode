//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func EvalAwait[T any](p *Page, js string) T {
	p.T.Helper()
	v, err := chromedp.Run(p.Ctx, chromedp.Evaluate[T](js, chromedp.EvalAwaitPromise))
	if err != nil {
		p.T.Fatalf("eval: %v", err)
	}
	return v
}

// Diagnostics for the macOS runner only (DO NOT MERGE): they log, never fail.

const diagEvents = `(() => {
	window.__ev = [];
	const d = (e) => { const t = e.target; return ((t && t.className && String(t.className)) || (t && t.nodeName) || "?").slice(0, 40); };
	const a = () => { const x = document.activeElement; return ((x && (x.className || x.nodeName)) || "none").toString().slice(0, 30); };
	for (const n of ["pointerdown", "pointerup", "pointercancel", "touchstart", "touchend", "touchcancel", "mousedown", "mouseup", "click", "focusin", "focusout", "gotpointercapture", "lostpointercapture", "fullscreenchange", "fullscreenerror"]) {
		document.addEventListener(n, (e) => window.__ev.push("cap " + n + "@" + d(e) + (e.pointerType ? "/" + e.pointerType : "") + " active=" + a()), true);
		window.addEventListener(n, (e) => window.__ev.push("bub " + n + (e.defaultPrevented ? " prevented" : "") + " active=" + a()), false);
	}
	return true;
})()`

func diagDump(t *testing.T, p *Page) {
	t.Helper()
	for _, e := range Eval[[]string](p, `window.__ev || []`) {
		t.Log(e)
	}
}

func TestZZDiagFullScreen(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	stubKeyboard(p)
	Eval[bool](p, diagEvents)
	Eval[bool](p, `(() => {
		const orig = Element.prototype.requestFullscreen;
		Element.prototype.requestFullscreen = function (o) {
			window.__ev.push("requestFullscreen called on " + (this.nodeName) + " activation=" + (navigator.userActivation && navigator.userActivation.isActive));
			const r = orig.call(this, o);
			r.then(() => window.__ev.push("requestFullscreen resolved"), (e) => window.__ev.push("requestFullscreen rejected: " + e));
			return r;
		};
		return true;
	})()`)
	t.Log("info:", Eval[string](p, `navigator.userAgent + " fsEnabled=" + document.fullscreenEnabled + " screen=" + screen.width + "x" + screen.height + " inner=" + innerWidth + "x" + innerHeight`))
	t.Log("button:", Eval[string](p, `(() => { const b = document.querySelector(".dterm-fullscreen"); if (!b) return "none"; const r = b.getBoundingClientRect(); const h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return JSON.stringify([r.left, r.top, r.width, r.height]) + " hit=" + (h && (h.className || h.nodeName)) + " same=" + (h && b.contains(h)); })()`))
	p.Tap(".dterm-fullscreen")
	time.Sleep(3 * time.Second)
	diagDump(t, p)
	t.Log("after:", Eval[string](p, `"fsEl=" + (document.fullscreenElement && document.fullscreenElement.nodeName) + " lock=" + window.__lock + " inner=" + innerWidth + "x" + innerHeight`))
}

func TestZZDiagTactileFirstTap(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden`)
	Eval[bool](p, diagEvents)
	t.Log("info:", Eval[string](p, `navigator.userAgent + " platform=" + navigator.platform + " maxTouch=" + navigator.maxTouchPoints + " coarse=" + matchMedia("(pointer: coarse)").matches + " active=" + (document.activeElement && document.activeElement.className)`))
	t.Log("key:", Eval[string](p, `(() => { const k = `+tkKey("L")+`; const r = k.getBoundingClientRect(); const h = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return JSON.stringify([r.left, r.top, r.width, r.height]) + " hit=" + (h && (h.className || h.nodeName)) + " inKey=" + (h && k.contains(h)); })()`))
	p.TapJS(tkKey("L"))
	time.Sleep(time.Second)
	diagDump(t, p)
	t.Log("dock hidden:", Eval[bool](p, `!(`+tkDock+`) || `+tkDock+`.hidden`))
	select {
	case m := <-c.in:
		t.Logf("hub got %s", show(m))
	case <-time.After(2 * time.Second):
		t.Log("hub got nothing")
	}
}

func TestZZDiagFrameRate(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	rate := `new Promise((done) => { const ts = []; const f = (t) => { ts.push(t); if (ts.length < 121) requestAnimationFrame(f); else { const d = ts.slice(1).map((x, i) => x - ts[i]).sort((a, b) => a - b); done("rAF gaps ms: median " + d[60].toFixed(1) + " p95 " + d[114].toFixed(1) + " max " + d[119].toFixed(1)); } }; requestAnimationFrame(f); })`
	t.Log("connect page:", EvalAwait[string](p, rate))
	p.Login("c-1")
	t.Log("app page:", EvalAwait[string](p, rate))
}
