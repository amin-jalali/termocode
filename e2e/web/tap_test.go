//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A tap lands on its target even while the view is still scrolling: Tap
// waits until the target stops moving, so a slow machine does not tap
// where the target was a moment ago. On the macOS runner a tap on the
// open tab (GoTab) starts a smooth scroll to the top that went on after
// Tap had read the target's place, and the theme test's "light" tap
// missed (2026-10-30). Here a scroll driven frame by frame stands for it:
// Tap's own scrollIntoView cannot stop it.
func TestTapHitsATargetThatIsStillMoving(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Login("c-1")
	p.GoTab("more")
	p.WaitJS(`(() => { const b = document.querySelector('[data-view="more"] .view-body'); return b.scrollHeight > b.clientHeight + 200; })()`)
	for _, scheme := range []string{"dark", "light", "dark", "light"} {
		n := map[string]string{"dark": "2", "light": "3"}[scheme]
		Eval[bool](p, `(() => {
			const b = document.querySelector('[data-view="more"] .view-body');
			b.scrollTop = b.scrollHeight;
			const end = performance.now() + 600;
			const step = () => {
				b.scrollTop = Math.max(0, b.scrollTop - 12);
				if (performance.now() < end) requestAnimationFrame(step);
			};
			requestAnimationFrame(step);
			return true;
		})()`)
		p.pickPref(1, n)
		p.WaitJS(`document.documentElement.dataset.theme === "` + scheme + `"`)
	}
}

// GoTab to the tab that is already open does not tap it: a tap on the
// open tab scrolls its view to the top (smoothly, slow on the macOS
// runner), and the theme test's second setScheme then tapped "Light"
// while the More view was still on its way up (FIX-42). Going to where
// the page already is leaves the view where it was.
func TestGoTabToTheOpenTabLeavesItsScroll(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Login("c-1")
	p.GoTab("more")
	body := `document.querySelector('[data-view="more"] .view-body')`
	p.WaitJS(`(() => { const b = ` + body + `; return b.scrollHeight > b.clientHeight + 200; })()`)
	Eval[bool](p, `(() => { const b = `+body+`; b.scrollTop = b.scrollHeight; return true; })()`)
	at := Eval[float64](p, body+`.scrollTop`)
	p.GoTab("more")
	// Ten frames: a smooth scroll started by a tap has moved by then.
	if _, err := chromedp.Run(p.Ctx, chromedp.Evaluate[bool](`new Promise((done) => { let n = 0; const f = () => (++n >= 10 ? done(true) : requestAnimationFrame(f)); requestAnimationFrame(f); })`, chromedp.EvalAwaitPromise)); err != nil {
		t.Fatal(err)
	}
	if got := Eval[float64](p, body+`.scrollTop`); got != at {
		t.Errorf("GoTab to the open More tab moved its view from %v to %v", at, got)
	}
}
