//go:build browser && webdev

package web

import (
	"testing"
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
