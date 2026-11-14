//go:build browser && webdev

package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// The phone approval sheet (MOBUX-12), on the component kit's real card
// dialogs: Deny first and focused, Allow arms after 600 ms, a destructive
// call offers only "Approve on laptop", a swipe down answers nothing, and
// an answer (never a Later) gives a short haptic buzz on the phone.

// fakeVibrate records every navigator.vibrate call in window.__buzz.
const fakeVibrate = `(() => {
	window.__buzz = [];
	Object.defineProperty(navigator, "vibrate", {configurable: true, value: (p) => { window.__buzz.push(JSON.stringify(p)); return true; }});
})()`

func openApprovalKit(t *testing.T, vp Viewport) *Page {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	p.onNewDocument(fakeVibrate)
	p.Do(chromedp.Navigate(s.Base + "/dev/kit"))
	p.WaitJS(ButtonByText("Open a tool card"))
	return p
}

func buzzes(p *Page) int { return Eval[int](p, `window.__buzz.length`) }

func footButtons(p *Page) []string {
	return Eval[[]string](p, `[...document.querySelectorAll(".sheet .card-foot button")].map((b) => b.textContent.trim())`)
}

// swipeSheetDown drags a touch from the sheet's grip down by px, in small
// steps, as a thumb does.
func (p *Page) swipeSheetDown(px float64) {
	p.T.Helper()
	b := Eval[struct{ X, Y float64 }](p, `(() => { const r = document.querySelector(".sheet .grip").getBoundingClientRect(); return {X: r.left + r.width / 2, Y: r.top + r.height / 2}; })()`)
	touch := func(ctx context.Context, tg *chromedp.Target, typ input.DispatchTouchEventType, y float64) error {
		id := 1.0
		pts := []*input.TouchPoint{}
		if typ != input.DispatchTouchEventTypeTouchEnd {
			pts = append(pts, &input.TouchPoint{X: b.X, Y: y, ID: &id})
		}
		_, err := cdp.Call(ctx, tg, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: typ, TouchPoints: pts})
		return err
	}
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		if err := touch(ctx, tg, input.DispatchTouchEventTypeTouchStart, b.Y); err != nil {
			return err
		}
		for i := 1; i <= 8; i++ {
			if err := touch(ctx, tg, input.DispatchTouchEventTypeTouchMove, b.Y+px*float64(i)/8); err != nil {
				return err
			}
		}
		return touch(ctx, tg, input.DispatchTouchEventTypeTouchEnd, b.Y+px)
	}))
}

// A destructive call on the phone: Deny first and focused, and the only
// other button is "Approve on laptop"; there is no Allow at all, armed or
// not, and the laptop button sends no answer.
func TestApprovalDestructiveOnlyOnLaptop(t *testing.T) {
	p := openApprovalKit(t, Phone)
	openCard(p, "destructive")
	p.Holds("true", 700*time.Millisecond) // past the arming: still no Allow
	got := footButtons(p)
	if len(got) != 2 || got[0] != "Deny" || got[1] != "Approve on laptop" {
		t.Fatalf("buttons %q, want Deny, Approve on laptop", got)
	}
	p.WaitJS(`document.activeElement === document.querySelector(".sheet .card-foot button")`)
	if Eval[bool](p, `!!document.querySelector(".sheet .card .btn-ai")`) {
		t.Fatal("a destructive card shows an Allow button")
	}
	p.TapJS(sheetButton("Approve on laptop"))
	p.WaitJS("!" + dialog)
	if got := toasts(p); got != "Shown on the laptop" {
		t.Errorf("after Approve on laptop the toasts say %q", got)
	}
	if n := buzzes(p); n != 0 {
		t.Errorf("handing a card to the laptop buzzed %d times", n)
	}
}

// A swipe down on the grip closes the sheet as Later: nothing is answered
// and nothing buzzes, even when the Allow button is armed.
func TestApprovalSwipeDownIsNoAnswer(t *testing.T) {
	p := openApprovalKit(t, Phone)
	openCard(p, "tool")
	p.WaitJS(`document.querySelector(".sheet .card .btn-ai.is-armed")`)
	p.swipeSheetDown(240)
	p.WaitJS("!" + dialog)
	if got := toasts(p); got != "Later" {
		t.Errorf("after a swipe down the toasts say %q, want Later only", got)
	}
	if n := buzzes(p); n != 0 {
		t.Errorf("a swipe down buzzed %d times", n)
	}
}

// A short swipe is not a close: the sheet stays and nothing is answered.
func TestApprovalShortSwipeKeepsSheet(t *testing.T) {
	p := openApprovalKit(t, Phone)
	openCard(p, "tool")
	p.swipeSheetDown(30)
	p.Holds(dialog+` !== null`, 300*time.Millisecond)
	if got := toasts(p); got != "" {
		t.Errorf("a short swipe said %q", got)
	}
}

// Each answer buzzes once on the phone: Deny, and Allow once after it is
// armed. Esc (Later) does not buzz. A desktop never buzzes.
func TestApprovalHapticOnAnswer(t *testing.T) {
	p := openApprovalKit(t, Phone)
	openCard(p, "tool")
	p.TapJS(sheetButton("Deny"))
	p.WaitJS("!" + dialog)
	if n := buzzes(p); n != 1 {
		t.Fatalf("Deny buzzed %d times, want 1", n)
	}
	openCard(p, "tool")
	p.WaitJS(`document.querySelector(".sheet .card .btn-ai.is-armed")`)
	p.TapJS(sheetButton("Allow once"))
	p.WaitJS("!" + dialog)
	if n := buzzes(p); n != 2 {
		t.Fatalf("Allow once buzzed %d times in all, want 2", n)
	}
	if got := toasts(p); !strings.Contains(got, "Allowed once.") {
		t.Errorf("toasts %q", got)
	}

	d := openApprovalKit(t, Desktop)
	openCard(d, "tool")
	d.TapJS(sheetButton("Deny"))
	d.WaitJS("!" + dialog)
	if n := buzzes(d); n != 0 {
		t.Errorf("the desktop buzzed %d times", n)
	}
}
