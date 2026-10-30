//go:build browser && webdev

package web

import (
	"testing"

	"github.com/chromedp/chromedp/kb"
)

// A button that needs the laptop reads Offline and is disabled while the
// link is down (web UX P5, 3.6). The kit never connects, so it is always
// in that state.
func TestNeedsLinkButtonLocksOffline(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p := openKit(t, vp)
			const btn = `document.querySelector(".btn.is-offline")`
			p.WaitJS(btn)
			if got := Eval[string](p, btn+`.textContent`); got != "Offline" {
				t.Errorf("a needs-link button reads %q offline, want Offline", got)
			}
			if got := Eval[string](p, btn+`.getAttribute("aria-disabled")`); got != "true" {
				t.Errorf("aria-disabled = %q, want true", got)
			}
		})
	}
}

// A card dialog's buttons are 44 px or more, sit in the thumb zone on a
// 390x844 phone (below 560 px), and at 200 % font size the card still
// fits the width with Deny on top.
func TestCardSheetFitsTheThumb(t *testing.T) {
	for _, vp := range []Viewport{Phone320, Phone} {
		t.Run(vp.Name, func(t *testing.T) {
			p := openKit(t, vp)
			for _, k := range cardKinds {
				openCard(p, k.kind)
				if bad := Eval[[]string](p, smallTargetsJS(".sheet .card-foot button")); len(bad) > 0 {
					t.Errorf("%s: card buttons under 44 px: %v", k.kind, bad)
				}
				if vp == Phone {
					if top := Eval[float64](p, `Math.min(...[...document.querySelectorAll(".sheet .card-foot button")].map((b) => b.getBoundingClientRect().top))`); top < 560 {
						t.Errorf("%s: card buttons start at %.0f px; want 560 or lower", k.kind, top)
					}
				}
				p.Key(kb.Escape)
				p.WaitJS("!" + dialog)
			}
			// The kit's own sticky header grows at 200 % and covers its
			// buttons, so the card opens first and the font grows under it.
			openCard(p, "tool")
			Eval[bool](p, `(document.documentElement.style.fontSize = "200%", true)`)
			p.noOverflow("200% tool card")
			order := Eval[[]float64](p, `[...document.querySelectorAll(".sheet .card-foot button")].map((b) => b.getBoundingClientRect().top)`)
			if len(order) == 2 && order[0] > order[1] {
				t.Errorf("at 200 %% Deny sits below Allow once (tops %v)", order)
			}
		})
	}
}
