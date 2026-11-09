//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (reload on a new build): termocode reloads into a new build and
// hands its gateway over: the page's link closes with 1012, which it
// retries (never the final bar), and gets back in with the token the tab
// holds: no new link. The hub of the new build names another build in
// meta, so the page loads itself again (the new build's files) and
// connects once more with the same token, online.
func TestReloadIntoNewBuildReconnectsAndLoadsIt(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			old := Meta("c-1")
			old.Build = "build-old"
			c := p.LoginMeta(old)
			Eval[int](p, `window.__beforeReload = 1`)

			c.Close(t, webproto.CloseRestarting) // the gateway hands over
			p.WaitJS(`document.documentElement.dataset.link === "offline"`)
			if Eval[bool](p, `document.querySelector(".bar-final") !== null`) {
				t.Fatal("1012 ended the session (final bar)")
			}

			// The next build's gateway: the same tokens, a new build.
			c2 := s.Hub.Accept(t)
			next := Meta("c-2")
			next.Build = "build-new"
			hello := c2.HandshakeMeta(t, next)
			if hello.Token == "" {
				t.Fatal("the page came back without its token")
			}
			// It loads the new build's page and connects again by itself.
			c3 := s.Hub.Accept(t)
			again := Meta("c-3")
			again.Build = "build-new"
			c3.HandshakeMeta(t, again)
			c3.Subs(t, "status")
			c3.Send(t, Snap("status", 1, statusData("after-build", 0)))
			p.WaitJS(`document.documentElement.dataset.link === "online"`)
			p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("after-build")`)
			if Eval[bool](p, `window.__beforeReload === 1`) {
				t.Error("the page did not load itself again for the new build")
			}
			// The same build again on a later reconnect is no reason to load.
			Eval[int](p, `window.__stay = 1`)
			c3.Drop()
			c4 := s.Hub.Accept(t)
			same := Meta("c-4")
			same.Build = "build-new"
			c4.HandshakeMeta(t, same)
			p.WaitJS(`document.documentElement.dataset.link === "online"`)
			s.Hub.NoConn(t, 800*time.Millisecond)
			if !Eval[bool](p, `window.__stay === 1`) {
				t.Error("the page loaded itself again for the same build")
			}
		})
	}
}
