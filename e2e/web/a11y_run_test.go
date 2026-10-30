//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Run, Test and Debug views (WEBP-4) pass the accessibility checker
// with content, on both screens and in both themes.
func TestAxeRunViews(t *testing.T) {
	src := axeSource(t)
	for _, vp := range Both {
		for _, theme := range []string{"dark", "light"} {
			t.Run(vp.Name+"/"+theme, func(t *testing.T) {
				s := StartServer(t)
				p := NewPage(t, s, vp)
				p.Media(map[string]string{"prefers-color-scheme": theme, "prefers-reduced-motion": "reduce"})
				c := p.Login("c-1")
				shotLike(t, c)
				for _, v := range []struct{ hash, ready string }{
					{"#/more/testing", `document.querySelectorAll(".tx-row").length === 4`},
					{"#/more/debug", `document.querySelectorAll(".dv-frame").length === 2`},
					{"#/more/services", `document.querySelectorAll(".svc").length === 3`},
					{"#/more/kube", `document.querySelectorAll(".kb-row").length === 5`},
				} {
					Eval[bool](p, `(location.hash = `+jsq(v.hash)+`, true)`)
					p.WaitJS(v.ready)
					p.axe(src, v.hash)
				}
				if !vp.Mobile {
					Eval[bool](p, `(location.hash = "#/more/debug", true)`)
					p.TapJS(`document.querySelector('[data-act="console"]')`)
					p.WaitJS(`document.querySelector(".dcon-row")`)
					p.axe(src, "debug console")
				}
			})
		}
	}
}

// shotLike answers the page from the run views' fixed data until the
// test ends: their snapshots on a sub, their reads on a request.
func shotLike(t *testing.T, c *HubConn) {
	t.Helper()
	snaps := runShotSnaps()
	go func() {
		for {
			var m webproto.Msg
			select {
			case m = <-c.in:
			case <-c.done:
				return
			}
			switch v := m.(type) {
			case *webproto.Sub:
				for _, tp := range v.Topics {
					if d, ok := snaps[tp]; ok {
						_ = c.send(Snap(tp, 1, d))
					}
				}
			case *webproto.Req:
				d, ok := runShotAnswer(v.Op)
				if !ok {
					_ = c.send(&webproto.ErrMsg{T: webproto.KindErr, ID: v.ID, Code: webproto.CodeNotFound, Msg: "Not in this test."})
					continue
				}
				_ = c.send(&webproto.Res{T: webproto.KindRes, ID: v.ID, Data: raw(d)})
			}
		}
	}()
}
