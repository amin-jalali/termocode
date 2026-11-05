//go:build browser

package web

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// popOut taps Pop out on the desktop terminal panel and returns the new
// tab as a Page (sized as a desktop) and its hub connection, after the
// handshake: the new tab never shows a sign-in.
func popOut(t *testing.T, p *Page) (*Page, *HubConn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	ch := chromedp.WaitNewTarget(ctx, func(i *target.Info) bool { return i.Type == "page" })
	p.Tap(".dterm-popout")
	var id target.ID
	select {
	case id = <-ch:
	case <-ctx.Done():
		t.Fatal("Pop out opened no tab")
	}
	pctx, pcancel := chromedp.NewContext(p.Ctx, chromedp.WithTargetID(id))
	t.Cleanup(pcancel)
	q := &Page{T: t, Ctx: pctx, S: p.S, VP: p.VP}
	q.Do(chromedp.EmulateViewport(p.VP.W, p.VP.H, chromedp.EmulateScale(p.VP.Scale)))
	c := p.S.Hub.Accept(t)
	c.Handshake(t, "c-2")
	return q, c
}

// WEBP-13: Pop out opens the focused shell in its own tab: a live view of
// the same laptop shell, on the same session (no second sign-in), with
// only the terminal on the page. Keys typed there go to that shell.
// Closing the tab ends its connection only: the shell stays (no
// term.close), and the first tab keeps showing it.
func TestDeskTermPopOutLiveAndCloseKeepsShell(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$ ls", "a.txt", "$"), termItem(3, "zsh", false, true))

	q, c2 := popOut(t, p)
	c2.Subs(t, "terms", "term:3")
	c2.Send(t, Snap("status", 1, statusData("main", 0)))
	c2.Send(t, Snap("toasts", 1, map[string]any{"toasts": []any{}}))
	c2.Send(t, Snap("terms", 1, termsSnap(termItem(3, "zsh", false, true))))
	c2.Send(t, Snap("term:3", 1, termSnap(3, 0, 80, "$ ls", "a.txt", "$")))
	q.WaitJS(`document.querySelector('.layout-solo [data-view="term"] .dterm-pane[data-buf="3"] .trow')`)
	if Eval[bool](q, `document.querySelector(".connect-foot, .signin, .workbench, .titlebar, .nav") !== null`) {
		t.Error("the popped-out tab shows more than the terminal (a sign-in, the workbench or a nav)")
	}
	if got := Eval[string](q, `location.hash`); got != "#/term/3?solo=1" {
		t.Errorf("popped-out tab at %q", got)
	}

	// Live: new rows from the laptop show in the new tab.
	c2.Send(t, Delta("term:3", 2, map[string]any{"kind": "lines", "first": 2, "last": 2, "lines": []string{"$ make"}, "tick": 4}))
	q.WaitJS(`[...document.querySelectorAll('[data-view="term"] .trow-text')].some((e) => e.textContent === "$ make")`)

	// Keys typed there go to shell 3.
	q.WaitJS(`document.activeElement === document.querySelector('[data-view="term"] .dterm-pane.is-focus .dterm-input')`)
	q.Key("x")
	r := expectReq(t, c2, "term.send")
	var a webproto.TermSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 3 || a.Text != "x" {
		t.Fatalf("term.send %s, want x to shell 3", r.Args)
	}
	c2.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})

	// Closing the tab ends its connection, and nothing else.
	if err := chromedp.Cancel(q.Ctx); err != nil {
		t.Fatalf("close the popped-out tab: %v", err)
	}
	for {
		select {
		case m := <-c2.in:
			if req, ok := m.(*webproto.Req); ok && req.Op == "term.close" {
				t.Fatal("closing the popped-out tab closed the shell")
			}
			continue
		case <-c2.Done():
		case <-time.After(wait):
			t.Fatal("the popped-out tab's connection stayed open after the tab closed")
		}
		break
	}
	c.Quiet(t, 300*time.Millisecond)
	if !Eval[bool](p, `document.querySelector('[data-view="term"] .dterm-pane[data-buf="3"] .trow') !== null`) {
		t.Error("the first tab lost the shell")
	}
}

// Pop out is there for every shell: two shells give two tabs, each its own.
func TestDeskTermPopOutTwoShellsTwoTabs(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true), termItem(9, "build", false, false))
	_, c2 := popOut(t, p)
	c2.Subs(t, "term:3")

	p.Tap(`.dterm-tab[data-buf="9"] .dterm-tab-open`)
	c.WaitSub(t, "term:9")
	c.Send(t, Snap("term:9", 1, termSnap(9, 0, 80, "building")))
	p.WaitJS(`document.querySelector('[data-view="term"] .dterm-pane.is-focus[data-buf="9"] .trow')`)
	_, c3 := popOut(t, p)
	c3.Subs(t, "term:9")
}
