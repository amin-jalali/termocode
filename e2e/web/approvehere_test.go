//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The page approves its own actions (owner 2026-11-08): with
// meta.approve_here an action that needs a confirm is sent at once; the
// hub's ask ev brings the laptop card's exact text, which the page shows
// Cancel first and focused, the action armed; the page answers the card
// itself with cards.answer. No "Ask the laptop", no waiting state.

// hereMeta is a controller's meta whose page approves here.
func hereMeta(conn string) *webproto.Meta {
	m := Meta(conn)
	m.ApproveHere = true
	return m
}

// hubAsks answers r as the hub does when the page approves: the ask ev
// with the card's text, then the card id.
func hubAsks(t *testing.T, c *HubConn, r *webproto.Req, id, body, action string) {
	t.Helper()
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAsk, Data: raw(webproto.AskCard{ID: id, Title: "Discard changes?", Body: body, Action: action})})
	reply(t, c, r, webproto.AskRef{Card: id})
}

// pageAnswers waits for the page's cards.answer and answers it as the hub
// does: res, then the answered ev to this page.
func pageAnswers(t *testing.T, c *HubConn, id, choice string) {
	t.Helper()
	reply(t, c, expectOp(t, c, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"`+choice+`"}`), nil)
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: id, By: "loc-test", Choice: choice})})
}

// approveHereDiscard runs the discard question of an open page: Cancel
// first, then confirm.
func approveHereDiscard(t *testing.T, p *Page, c *HubConn, open func(), paths string) {
	t.Helper()
	const body = "Discard changes in src/a.go? This cannot be undone."
	open()
	r := expectOp(t, c, webproto.OpGitDiscard, `{"paths":[`+paths+`]}`)
	hubAsks(t, c, r, "ask-1", body, "Discard")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === ` + jsq(body) + ` && document.activeElement === document.querySelector(".ask .choices button")`)
	if n := Eval[int](p, `[...document.querySelectorAll(".sheet button")].filter((b) => b.textContent.includes("Ask the laptop")).length`); n != 0 {
		t.Fatal("the page asks the laptop although it approves here")
	}
	if Eval[bool](p, `!!document.querySelector(".waiting")`) {
		t.Fatal("the page waits for the laptop")
	}
	p.Key(kb.Enter) // the focused button is Cancel
	pageAnswers(t, c, "ask-1", "cancel")
	p.WaitJS(`!document.querySelector(".ask") && document.querySelector(".toasts")?.textContent.includes("Cancelled. Nothing changed.")`)

	open()
	r = expectOp(t, c, webproto.OpGitDiscard, "")
	hubAsks(t, c, r, "ask-2", body, "Discard")
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")?.textContent === "Discard"`)
	p.TapJS(`[...document.querySelectorAll(".ask button")].find((b) => b.textContent === "Discard")`)
	pageAnswers(t, c, "ask-2", "confirm")
	p.WaitJS(`!document.querySelector(".ask") && document.querySelector(".toasts")?.textContent.includes("Done")`)
	c.Quiet(t, 200*time.Millisecond)
	if pr := p.Problems(); len(pr) > 0 {
		t.Errorf("problems: %q", pr)
	}
}

func TestPhoneApprovesHere(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(hereMeta("c-git"))
	p.GoTab("git")
	waitGitSub(t, p, c)
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('` + rowSel("src/a.go") + `')`)
	p.TapJS(`document.querySelector('` + rowSel("src/a.go") + ` .git-open')`)
	reply(t, c, expectOp(t, c, webproto.OpGitDiff, ""), webproto.GitDiff{Path: "src/a.go", Rows: []webproto.DiffRow{}})
	approveHereDiscard(t, p, c, func() { p.TapJS(ButtonByText("Discard changes…")) }, `"src/a.go"`)
	p.noOverflow("approve here")
}

func TestDeskApprovesHere(t *testing.T) {
	p, c := openDeskSCM(t, hereMeta("c-scm"))
	approveHereDiscard(t, p, c, func() {
		Eval[bool](p, scmRow("changes", "src/a.go")+`.querySelector('[aria-label="Discard Changes"]').click() ?? true`)
	}, `"src/a.go"`)
}

// Whichever view sent the request (a palette command, a sheet's action,
// a view without its own question), the page shows the hub's card from
// the ask ev, Cancel first, and answers it itself; on the phone and on
// the desktop.
func TestApproveHereFromAnyView(t *testing.T) {
	for _, vp := range []Viewport{Phone, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			c := p.LoginMeta(hereMeta("c-any"))
			const body = "Run on baaytona (deploy@65.109.212.14):\n\nssh -p 22 -- deploy@65.109.212.14"
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAsk, Data: raw(webproto.AskCard{ID: "ask-9", Title: "Open a terminal?", Body: body, Action: "Open"})})
			p.WaitJS(`document.querySelector(".ask-text")?.textContent === ` + jsq(body) + ` && document.activeElement?.textContent === "Cancel"`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")?.textContent === "Open"`)
			p.TapJS(`[...document.querySelectorAll(".ask button")].find((b) => b.textContent === "Open")`)
			pageAnswers(t, c, "ask-9", "confirm")
			p.WaitJS(`!document.querySelector(".ask")`)
			if pr := p.Problems(); len(pr) > 0 {
				t.Errorf("problems: %q", pr)
			}
		})
	}
}
