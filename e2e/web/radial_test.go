//go:build browser

package web

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// The floating radial menu (RM-2) on a touch phone and a touch tablet:
// the button, its rings, the page's own ops behind the actions, the held
// action's card with ★ favorite kept on the device; never on a desktop.

// TabletTouch is a tablet under the workbench's width, with touch.
var TabletTouch = Viewport{Name: "tablet-touch", W: 1024, H: 768, Scale: 1, Mobile: true}

// polarPt is the design point at radius r and angle deg (0° = top, clockwise).
func polarPt(r, deg float64) (float64, float64) {
	a := (deg - 90) * math.Pi / 180
	return 300 + r*math.Cos(a), 300 + r*math.Sin(a)
}

// touchHold holds one finger at (x, y) for d.
func touchHold(x, y float64, d time.Duration) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		id := 1.0
		if _, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: input.DispatchTouchEventTypeTouchStart, TouchPoints: []*input.TouchPoint{{X: x, Y: y, ID: &id}}}); err != nil {
			return err
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: input.DispatchTouchEventTypeTouchEnd, TouchPoints: []*input.TouchPoint{}})
		return err
	})
}

// radialAt is the page point of a design point (x, y) of the open menu.
func radialAt(p *Page, x, y float64) (float64, float64) {
	p.T.Helper()
	pt := Eval[[2]float64](p, fmt.Sprintf(`(() => { const r = document.querySelector(".rm-overlay .rm-stage").getBoundingClientRect(); const s = (r.width - 36) / 600; return [r.left + 18 + %f * s, r.top + 18 + %f * s]; })()`, x, y))
	return pt[0], pt[1]
}

// radialCat taps category id on the inner ring.
func radialCat(p *Page, id string) {
	p.T.Helper()
	i := Eval[int](p, `[...document.querySelectorAll(".rm-overlay .rm-cat")].findIndex((e) => e.dataset.id === "`+id+`")`)
	n := Eval[int](p, `document.querySelectorAll(".rm-overlay .rm-cat").length`)
	if i < 0 {
		p.T.Fatalf("no category %s", id)
	}
	x, y := polarPt(86, 180+float64(i)*360/float64(n))
	px, py := radialAt(p, x, y)
	p.Do(chromedp.TapXY(px, py))
	p.WaitJS(`document.querySelector('.rm-overlay .rm-cat[data-id="` + id + `"]')?.getAttribute("aria-checked") === "true"`)
}

// radialItem is the page point of action id on the outer ring.
func radialItem(p *Page, id string) (float64, float64) {
	p.T.Helper()
	i := Eval[int](p, `[...document.querySelectorAll(".rm-overlay .rm-item")].findIndex((e) => e.dataset.id === "`+id+`")`)
	// the outer ring has at least six wedges (empty ones too)
	n := Eval[int](p, `+(document.querySelector(".rm-overlay .rm-outer")?.dataset.slots || document.querySelectorAll(".rm-overlay .rm-item").length)`)
	if i < 0 {
		p.T.Fatalf("no action %s", id)
	}
	x, y := polarPt(170, float64(i)*360/float64(n))
	return radialAt(p, x, y)
}

func openRadial(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-radial")
	p.WaitJS(`document.querySelector(".rm-fab") && getComputedStyle(document.querySelector(".rm-fab")).position === "absolute"`)
	return p, c
}

func tapRadial(p *Page) {
	p.T.Helper()
	p.Tap(".rm-fab")
	p.WaitJS(`document.querySelector(".rm-overlay .rm-stage")`)
}

// The button sits bottom-right, clear of the tab bar; a tap opens the
// rings over an invisible catcher; a VIEW action opens its view.
func TestRadialOpensViews(t *testing.T) {
	for _, vp := range []Viewport{Phone, TabletTouch} {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openRadial(t, vp)
			r := Eval[[4]float64](p, `(() => { const r = document.querySelector(".rm-fab").getBoundingClientRect(); return [r.left, r.top, r.width, r.height]; })()`)
			if r[2] != 64 || r[0]+64 > float64(vp.W)-12 || r[0] < float64(vp.W)/2 {
				t.Errorf("the button is at %v, want 64 px at the right", r)
			}
			if bar := Eval[float64](p, `(() => { const b = document.querySelector(".tabbar"); return b ? b.getBoundingClientRect().top : innerHeight; })()`); r[1]+64 > bar {
				t.Errorf("the button (bottom %.0f) covers the tab bar (top %.0f)", r[1]+64, bar)
			}
			if got := Eval[string](p, `document.querySelector(".rm-fab").getAttribute("aria-label")`); got != "Radial menu" {
				t.Errorf("label %q", got)
			}
			tapRadial(p)
			if n := Eval[int](p, `document.querySelectorAll(".rm-overlay .rm-cat").length`); n != 7 {
				t.Errorf("%d categories, want SAVED + 6", n)
			}
			if bg := Eval[string](p, `getComputedStyle(document.querySelector(".rm-overlay")).backgroundColor`); bg != "rgba(0, 0, 0, 0)" {
				t.Errorf("the catcher dims the page: %s", bg)
			}
			radialCat(p, "view")
			x, y := radialItem(p, "view-term")
			p.Do(chromedp.TapXY(x, y))
			p.WaitJS(`location.hash.startsWith("#/term")`)
			p.WaitJS(`!document.querySelector(".rm-overlay") && !document.querySelector(".rm-fab").hidden`)
			tapRadial(p)
			radialCat(p, "tools")
			x, y = radialItem(p, "tools-settings")
			p.Do(chromedp.TapXY(x, y))
			p.WaitJS(`location.hash === "#/more/settings"`)
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("page problems (CSP, Trusted Types, errors): %q", probs)
			}
		})
	}
}

// New chat sends the page's chat.new; an outside tap closes.
func TestRadialNewChat(t *testing.T) {
	p, c := openRadial(t, Phone)
	tapRadial(p)
	radialCat(p, "ai")
	x, y := radialItem(p, "ai-new")
	p.Do(chromedp.TapXY(x, y))
	expectReq(t, c, "chat.new")
	tapRadial(p)
	p.Do(chromedp.TapXY(20, 120))
	p.WaitJS(`!document.querySelector(".rm-overlay")`)
}

// A hold opens the card; ★ favorite saves the action on this device: it
// is in SAVED after a reload.
func TestRadialFavoriteStays(t *testing.T) {
	p, _ := openRadial(t, Phone)
	tapRadial(p)
	radialCat(p, "tools")
	x, y := radialItem(p, "tools-kube")
	p.Do(touchHold(x, y, 700*time.Millisecond))
	p.WaitJS(`document.querySelector(".rm-tip:not([hidden]) .rm-tip-title")?.textContent === "kubernetes"`)
	p.Tap(".rm-tip-fav")
	p.WaitJS(`document.querySelector(".rm-tip-fav").textContent.includes("favorited")`)
	if got := Eval[string](p, `localStorage.getItem("termocode.radial.favorites")`); !strings.Contains(got, "tools-kube") {
		t.Fatalf("favourites kept as %q", got)
	}
	p.Reload()
	p.WaitJS(`document.querySelector(".rm-fab")`)
	tapRadial(p)
	p.WaitJS(`[...document.querySelectorAll(".rm-overlay .rm-item")].some((e) => e.dataset.id === "tools-kube")`)
}

// A desktop never loads the radial.
func TestRadialNeverOnDesktop(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Login("c-radial-desk")
	p.Holds(`!document.querySelector(".rm-fab")`, 1500*time.Millisecond)
	for _, r := range p.Requests() {
		if strings.Contains(r, "radial.js") {
			t.Fatalf("the desktop loaded %s", r)
		}
	}
}

// The button moves clear of a bar that comes without a route change (the
// terminal's key bar) and of a tablet's rail on a phone turned sideways.
func TestRadialClearOfBars(t *testing.T) {
	p, c := openTerm(t, Phone)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.WaitJS(`document.querySelector(".keybar") && document.querySelector(".rm-fab")`)
	p.WaitJS(`document.querySelector(".rm-fab").getBoundingClientRect().bottom <= document.querySelector(".keybar").getBoundingClientRect().top`)
}

// A sheet over the page hides the button (it would sit on the sheet's
// buttons); it comes back when the sheet closes.
func TestRadialHidesUnderSheets(t *testing.T) {
	p, c := openRadial(t, Phone)
	p.GoTab("git")
	c.WaitSub(t, "git")
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('.gp-row[data-path="b.go"]')`)
	gitViews(t, Phone, c, noViews)
	tapRadial(p)
	radialCat(p, "git")
	x, y := radialItem(p, "git-branch")
	p.Do(chromedp.TapXY(x, y))
	p.WaitJS(`document.querySelector(".sheet-layer")`)
	p.WaitJS(`getComputedStyle(document.querySelector(".rm-root")).display === "none"`)
	p.TapJS(`document.querySelector(".sheet-layer [data-act=\"close\"]")`) // the Branches sheet's ✕
	p.WaitJS(`!document.querySelector(".sheet-layer") && getComputedStyle(document.querySelector(".rm-root")).display !== "none"`)
}
