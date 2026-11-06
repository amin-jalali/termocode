//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// front brings p's tab to the front: a tab behind another runs no
// animation frames, and the page's taps wait for them.
func front(p *Page) {
	p.T.Helper()
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.BringToFront, cdp.Empty{})
		return err
	}))
}

// popOut taps Pop out on the desktop terminal panel and returns the new
// tab as a Page (sized as a desktop) and its hub connection, after the
// handshake: the new tab never shows a sign-in.
func popOut(t *testing.T, p *Page) (*Page, *HubConn) {
	t.Helper()
	before := map[target.ID]bool{}
	for _, x := range targets(t, p) {
		before[x.TargetID] = true
	}
	p.Tap(".dterm-popout")
	// The new tab has loaded once it connects: look for it after that.
	c := p.S.Hub.Accept(t)
	c.Handshake(t, "c-2")
	var id target.ID
	for _, x := range targets(t, p) {
		if x.Type == "page" && x.OpenerID != "" && !before[x.TargetID] {
			id = x.TargetID
		}
	}
	if id == "" {
		t.Fatal("Pop out opened no tab")
	}
	pctx, pcancel := chromedp.NewContext(p.Ctx, chromedp.WithTargetID(id))
	t.Cleanup(pcancel)
	// The first run attaches the tab for the context's whole life: it
	// runs on pctx itself, not on a context with a time limit.
	if err := chromedp.Do(pctx, chromedp.EmulateViewport(p.VP.W, p.VP.H, chromedp.EmulateScale(p.VP.Scale))); err != nil {
		t.Fatalf("attach to the new tab: %v", err)
	}
	q := &Page{T: t, Ctx: pctx, S: p.S, VP: p.VP}
	front(q)
	return q, c
}

// popReq returns the next request for op; subs, unsubs and paint
// requests on the way are skipped (the new tab may start narrow and
// change its view when it is sized as a desktop).
func popReq(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Req:
			if m.Op == op {
				return m
			}
			if !paintOps[m.Op] {
				t.Fatalf("page sent %s; want %s", show(m), op)
			}
		case *webproto.Sub, *webproto.Unsub:
		default:
			t.Fatalf("page sent %s; want %s", show(m), op)
		}
	}
}

// popSub reads until a sub that names topic, skipping what popReq skips.
func popSub(t *testing.T, c *HubConn, topic string) {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Sub:
			if slices.Contains(m.Topics, topic) {
				return
			}
		case *webproto.Unsub:
		case *webproto.Req:
			if !paintOps[m.Op] {
				t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
			}
		default:
			t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
		}
	}
}

// settle waits until the new tab shows the desktop pane of buf and has
// asked for its rows, then reads what it sent on the way: a headless new
// tab starts narrow, and its view changes once it is sized as a desktop.
func settle(t *testing.T, q *Page, c *HubConn, buf int) {
	t.Helper()
	q.WaitJS(fmt.Sprintf(`document.querySelector('.layout-solo [data-view="term"] .dterm-pane[data-buf="%d"]')`, buf))
	popSub(t, c, fmt.Sprint("term:", buf))
	for {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && !paintOps[r.Op] {
				t.Fatalf("the new tab sent %s at start", show(m))
			}
			continue
		case <-time.After(300 * time.Millisecond):
		}
		return
	}
}

// targets lists the browser's targets.
func targets(t *testing.T, p *Page) []*target.Info {
	t.Helper()
	ctx, cancel := context.WithTimeout(p.Ctx, wait)
	defer cancel()
	ts, err := chromedp.Targets(ctx)
	if err != nil {
		t.Fatalf("list the tabs: %v", err)
	}
	return ts
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
	settle(t, q, c2, 3)
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
	r := popReq(t, c2, "term.send")
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
	for end := time.After(300 * time.Millisecond); ; {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && !paintOps[r.Op] {
				t.Fatalf("the first tab sent %s when the other tab closed", show(m))
			}
			continue
		case <-end:
		}
		break
	}
	front(p)
	if !Eval[bool](p, `document.querySelector('[data-view="term"] .dterm-pane[data-buf="3"] .trow') !== null`) {
		t.Error("the first tab lost the shell")
	}
}

// Pop out is there for every shell: two shells give two tabs, each its own.
func TestDeskTermPopOutTwoShellsTwoTabs(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true), termItem(9, "build", false, false))
	q2, c2 := popOut(t, p)
	settle(t, q2, c2, 3)

	front(p)
	p.Tap(`.dterm-tab[data-buf="9"] .dterm-tab-open`)
	popSub(t, c, "term:9")
	c.Send(t, Snap("term:9", 1, termSnap(9, 0, 80, "building")))
	p.WaitJS(`document.querySelector('[data-view="term"] .dterm-pane.is-focus[data-buf="9"] .trow')`)
	q3, c3 := popOut(t, p)
	settle(t, q3, c3, 9)
}
