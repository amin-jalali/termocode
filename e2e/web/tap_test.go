//go:build browser

package web

import "testing"

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
		p.Tap(`.more .field:nth-of-type(1) .seg-item:nth-child(` + n + `)`)
		p.WaitJS(`document.documentElement.dataset.theme === "` + scheme + `"`)
	}
}
