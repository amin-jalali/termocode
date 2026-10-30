//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The card safety pack (WEB-15, WEB-UX-3, web UX P1, rule AH-1) on the
// real cards view: the cards come from the cards topic of a fake hub and
// every answer is a cards.answer request the test reads and replies to.
// For every kind, at 390 px with touch and at 1280 px with a mouse.

// kindCard is a card of kind k as the hub sends it.
func kindCard(id, k string) map[string]any {
	switch k {
	case "mcp":
		c := toolCard(id, "Run task: build\n\nAsked by an outside agent over MCP (tool run_task).")
		c["kind"], c["title"] = "mcp", `codex wants to run task "build"`
		return c
	case "plan":
		return map[string]any{"id": id, "kind": "plan", "title": "Approve this plan?", "body": "1. Read quit.go\n2. Fix the test", "since": 1700000000000,
			"choices": []any{
				map[string]any{"id": "deny", "label": "Deny", "safe": true},
				map[string]any{"id": "approve", "label": "Approve"},
				map[string]any{"id": "edit", "label": "Edit", "needs_laptop": true},
			}}
	case "review":
		return map[string]any{"id": id, "kind": "review", "title": "Review AI change: a.go", "body": "AI edit", "file": "a.go", "since": 1700000000000,
			"choices": []any{
				map[string]any{"id": "reject", "label": "Reject", "safe": true},
				map[string]any{"id": "accept", "label": "Accept"},
			},
			"diff": map[string]any{"added": 1, "removed": 1, "rows": []any{
				map[string]any{"k": "ctx", "text": "package a", "old": 1, "new": 1},
				map[string]any{"k": "del", "text": "x := 1", "old": 2},
				map[string]any{"k": "add", "text": "x := 2", "new": 2},
			}}}
	case "trust":
		return trustCard(id)
	}
	return toolCard(id, "go test ./internal/app/ -run TestQuit")
}

// choiceIDs are the safe and the risky choice ids of each kind.
var choiceIDs = map[string][2]string{
	"tool": {"deny", "allow"}, "mcp": {"deny", "allow"}, "plan": {"deny", "approve"}, "review": {"reject", "accept"},
}

// openCardView logs in, sends one waiting card of kind k, taps the pill
// and waits until the card dialog has the focus.
func openCardView(t *testing.T, vp Viewport, k string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	// No sheet rise: the tap must land where the button will stay.
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("cards", 1, cardsData(kindCard("card-1", k))))
	reopenCard(p)
	return p, c
}

// reopenCard opens the card sheet again from the pill.
func reopenCard(p *Page) {
	p.T.Helper()
	p.WaitJS("!" + dialog)
	p.Tap(".attention")
	p.WaitJS(dialog + ` && ` + dialog + `.contains(document.activeElement)`)
}

// paintOps are requests the page sends on its own to colour what it
// shows (syntax, terminal colours); a test that does not look for one
// skips it.
var paintOps = map[string]bool{"buf.hl": true, "term.colors": true}

// nextReq returns the page's next request; subs and paint requests on the
// way are skipped.
func nextReq(t *testing.T, c *HubConn) *webproto.Req {
	t.Helper()
	return nextReqOr(t, c, "")
}

// nextReqOr is nextReq that also returns a paint request for op.
func nextReqOr(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	for {
		m := c.Next(t)
		if r, ok := m.(*webproto.Req); ok {
			if paintOps[r.Op] && r.Op != op {
				continue
			}
			return r
		}
		if _, ok := m.(*webproto.Sub); !ok {
			t.Fatalf("page sent %s; want a request", show(m))
		}
	}
}

// expectAnswer reads a cards.answer for card-1 with choice and replies ok.
func expectAnswer(t *testing.T, c *HubConn, choice string) {
	t.Helper()
	r := nextReq(t, c)
	var a webproto.CardsAnswerArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || r.Op != "cards.answer" || a.ID != "card-1" || a.Choice != choice {
		t.Fatalf("page sent %s; want cards.answer card-1 %s", show(r), choice)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
}

func forEachCardView(t *testing.T, f func(t *testing.T, vp Viewport, k cardKind)) {
	for _, vp := range Both {
		for _, k := range cardKinds {
			t.Run(vp.Name+"/"+k.kind, func(t *testing.T) { f(t, vp, k) })
		}
	}
}

// The safe answer is the first button and has the focus; a trust card
// has no answer button at all; no card says "always".
func TestCardViewSafeChoiceFirstAndFocused(t *testing.T) {
	forEachCardView(t, func(t *testing.T, vp Viewport, k cardKind) {
		p, _ := openCardView(t, vp, k.kind)
		got := Eval[[]string](p, `[...document.querySelectorAll(".sheet .card-foot button")].map((b) => b.textContent.trim())`)
		want := k.safe
		if k.kind == "trust" {
			want = "Later"
		}
		if len(got) == 0 || got[0] != want {
			t.Fatalf("buttons %q, want %q first", got, want)
		}
		p.WaitJS(`document.activeElement === document.querySelector(".sheet .card-foot button")`)
		if k.risky != "" && !strings.Contains(strings.Join(got, "|"), k.risky) {
			t.Errorf("buttons %q have no %q", got, k.risky)
		}
		if k.risky == "" && Eval[bool](p, riskyBtn+` !== null`) {
			t.Error("a trust card has an answer button")
		}
		if txt := Eval[string](p, dialog+`.innerText`); strings.Contains(strings.ToLower(txt), "always") {
			t.Errorf("the card says always: %q", txt)
		}
	})
}

// Enter presses the focused button, which is the safe one.
func TestCardViewEnterIsTheSafeAnswer(t *testing.T) {
	forEachCardView(t, func(t *testing.T, vp Viewport, k cardKind) {
		p, c := openCardView(t, vp, k.kind)
		p.WaitJS(`document.activeElement === document.querySelector(".sheet .card-foot button")`)
		p.Holds("true", 700*time.Millisecond) // armed by now: Enter must still not allow
		p.Key(kb.Enter)
		if k.risky == "" {
			p.WaitJS("!" + dialog)
			c.Quiet(t, 300*time.Millisecond)
			return
		}
		expectAnswer(t, c, choiceIDs[k.kind][0])
		p.WaitJS("!" + dialog)
		p.WaitJS(`(` + toastsJS + `).includes(` + jsq(k.said) + `)`)
		if got := toasts(p); strings.Contains(got, k.done) {
			t.Errorf("after Enter the toasts say %q", got)
		}
	})
}

// Esc means Later: the sheet closes, nothing is sent, the pill stays.
func TestCardViewEscIsLater(t *testing.T) {
	forEachCardView(t, func(t *testing.T, vp Viewport, k cardKind) {
		p, c := openCardView(t, vp, k.kind)
		p.Holds("true", 700*time.Millisecond)
		p.Key(kb.Escape)
		p.WaitJS("!" + dialog)
		c.Quiet(t, 300*time.Millisecond)
		p.WaitJS(`document.querySelector(".attention")`)
		if got := toasts(p); got != "" {
			t.Errorf("after Esc the toasts say %q, want none", got)
		}
	})
}

// A press that begins in the first 600 ms sends nothing; once armed the
// same press sends the risky answer.
func TestCardViewRiskyIgnoredBeforeArming(t *testing.T) {
	forEachCardView(t, func(t *testing.T, vp Viewport, k cardKind) {
		if k.risky == "" {
			t.Skip("a trust card has no risky answer")
		}
		p, c := openCardView(t, vp, k.kind)
		early := false
		for try := 0; try < 3 && !early; try++ {
			if try > 0 {
				p.Key(kb.Escape)
				reopenCard(p)
			}
			Eval[bool](p, recordPress(riskyBtn))
			p.TapJS(riskyBtn)
			early = pressedEarly(p)
		}
		if !early {
			t.Skip("this machine never got a tap in within 600 ms")
		}
		c.Quiet(t, 800*time.Millisecond)
		p.WaitJS(riskyBtn + `.classList.contains("is-armed")`)
		p.TapJS(riskyBtn)
		expectAnswer(t, c, choiceIDs[k.kind][1])
		p.WaitJS(`(` + toastsJS + `).includes(` + jsq(k.done) + `)`)
		p.WaitJS("!" + dialog)
	})
}

// The laptop (or another browser) answers first: the buttons go, a line
// says who answered, and Close closes.
func TestCardViewAnsweredElsewhere(t *testing.T) {
	for _, tc := range []struct{ by, want string }{{"laptop", "Answered on the laptop."}, {"phone-2", "Answered on phone-2."}} {
		for _, vp := range Both {
			t.Run(vp.Name+"/"+tc.by, func(t *testing.T) {
				p, c := openCardView(t, vp, "tool")
				c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(map[string]any{"id": "card-1", "by": tc.by})})
				c.Send(t, Delta("cards", 2, map[string]any{"op": "remove", "id": "card-1", "by": tc.by}))
				p.WaitJS(dialog + `?.innerText.includes(` + jsq(tc.want) + `)`)
				if Eval[bool](p, riskyBtn+` !== null`) {
					t.Error("an answered card still has the risky button")
				}
				p.WaitJS(`document.activeElement?.textContent.trim() === "Close"`)
				p.TapJS(ButtonByText("Close"))
				p.WaitJS("!" + dialog + ` && !document.querySelector(".attention")`)
			})
		}
	}
}

// The hub says the card is gone (answered a moment before): the sheet
// says so instead of pretending the answer went through.
func TestCardViewGone(t *testing.T) {
	p, c := openCardView(t, Phone, "tool")
	p.TapJS(ButtonByText("Deny"))
	r := nextReq(t, c)
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeGone, Msg: "This card was answered already."})
	p.WaitJS(dialog + `?.innerText.includes("This card is gone.")`)
	if got := toasts(p); strings.Contains(got, "Denied") {
		t.Errorf("toasts %q claim the answer went through", got)
	}
}

// Without the link a card shows "Waiting for the connection…" and no
// answer button; when the link is back the buttons come back.
func TestCardViewOfflineWaits(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openCardView(t, vp, "tool")
			c.Drop()
			p.WaitJS(dialog + `?.innerText.includes("Waiting for the connection")`)
			if Eval[bool](p, riskyBtn+` !== null`) {
				t.Error("an offline card still has the risky button")
			}
			c2 := p.S.Hub.Accept(t)
			c2.Handshake(t, "c-1")
			p.WaitJS(riskyBtn + ` !== null`)
		})
	}
}

// A review card shows its diff rows, left to right.
func TestCardViewReviewShowsDiff(t *testing.T) {
	p, _ := openCardView(t, Phone, "review")
	if n := Eval[int](p, `document.querySelectorAll(".sheet .drow").length`); n != 3 {
		t.Errorf("the review card shows %d diff rows, want 3", n)
	}
	if got := p.Text(".sheet .drow-add .drow-text"); got != "x := 2" {
		t.Errorf("added row %q", got)
	}
}
