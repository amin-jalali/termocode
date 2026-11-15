//go:build browser

package web

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// The owner's iPhone (2026-11-15): after the web panel stopped and
// started again, a tap on the second shell opened the old key row and
// the system keyboard, not the Tactile keyboard. The keyboard's file
// could not load while the panel was down, and the page never tried
// again. Now a tap on the terminal loads it again and docks it.
func TestPhoneTermTactileOpensAfterReconnect(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, BigPhone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "light"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	items := []map[string]any{termItem(3, "fu 1", false, true), termItem(9, "web 2", false, false)}
	// The panel is down: the keyboard's file does not load.
	blocked := func(on bool) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
			pat := []*network.BlockPattern{}
			if on {
				pat = append(pat, &network.BlockPattern{URLPattern: "*://*:*/assets/tactile.js", Block: true})
			}
			_, err := cdp.Call(ctx, tg, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: pat})
			return err
		})
	}
	p.Do(blocked(true))
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), items...)
	p.TapJS(termView + `.querySelector('.term-chip[data-buf="9"] .chip')`)
	subSkipping(t, c, "term:9")
	c.Send(t, Snap("term:9", 1, termSnap(9, 0, 80, "$ ")))
	p.WaitJS(termView + `.querySelector(".pterm-x textarea")`)
	c2, _ := reconnect(t, p, c, "c-1")
	p.Do(blocked(false))
	c2.Send(t, Snap("terms", 1, termsSnap(items...)))
	c2.Send(t, Snap("term:9", 1, termSnap(9, 0, 80, "$ ")))
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden`)
	if got := Eval[string](p, xtermInput+`.getAttribute("inputmode")`); got != "none" {
		t.Errorf("the terminal's inputmode is %q, want none (the Tactile keyboard)", got)
	}
	if Eval[bool](p, `!!document.querySelector('[data-view="term"] .keybar')?.offsetHeight`) {
		t.Error("the old key row shows with the Tactile keyboard up")
	}
}
