//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Cards inline in the phone's Home (MOB-6): the waiting cards sit in the
// Home stream with the sheet's rules (safe choice first, risky arms for
// 600 ms, Esc / swipe = Later, first answer wins), #/home?card=<id> opens
// the sheet over the Home, and an answer gives a short haptic where the
// phone has one.

const (
	homeCard     = `document.querySelector('.home-waiting .card-inline')`
	homeRisky    = `document.querySelector('.home-waiting .card-inline .btn-ai')`
	homeSafe     = `document.querySelector('.home-waiting .card-inline .card-foot button')`
	vibrateCount = `(window.__buzz ?? 0)`
	stubVibrate  = `(window.__buzz = 0, navigator.vibrate = () => { window.__buzz++; return true; }, true)`
)

// openHome logs in on the phone, sends the cards and opens the Home.
func openHome(t *testing.T, cards ...map[string]any) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("cards", 1, cardsData(cards...)))
	Eval[bool](p, stubVibrate)
	Eval[bool](p, `(location.hash = "#/home", true)`)
	p.WaitJS(homeCard)
	// The Home asks for the saved views once it is live (MOBUX-18).
	if r := nextReq(t, c); r.Op != "view.list" {
		t.Fatalf("page sent %s; want view.list", show(r))
	} else {
		c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
	}
	return p, c
}

// The inline card puts the safe answer first; a tap on it answers with
// the safe choice and buzzes once.
func TestHomeInlineCardSafeFirstByTouch(t *testing.T) {
	p, c := openHome(t, toolCard("card-1", "go test ./..."))
	got := Eval[[]string](p, `[...document.querySelectorAll(".home-waiting .card-inline .card-foot button")].map((b) => b.textContent.trim())`)
	if len(got) == 0 || got[0] != "Deny" {
		t.Fatalf("inline buttons %q, want Deny first", got)
	}
	if Eval[bool](p, `document.activeElement === `+homeSafe) {
		t.Error("an inline card took the focus; only the sheet does")
	}
	p.Tap(".home-waiting .card-inline .card-foot button")
	expectAnswer(t, c, "deny")
	p.WaitJS(vibrateCount + ` === 1`)
}

// The risky answer of an inline card is inert for 600 ms, as in the sheet.
func TestHomeInlineCardRiskyArms(t *testing.T) {
	p, c := openHome(t, toolCard("card-1", "go test ./..."))
	if !Eval[bool](p, homeRisky+`.getAttribute("aria-disabled") === "true"`) {
		t.Fatal("the risky inline button is armed at once")
	}
	p.WaitJS(homeRisky + `.getAttribute("aria-disabled") === null`)
	p.TapJS(homeRisky)
	expectAnswer(t, c, "allow")
}

// By keyboard: Esc on an inline card is Later: it leaves the stream and
// nothing is sent. Tab reaches the next card's Deny and Enter answers it.
func TestHomeInlineCardKeyboard(t *testing.T) {
	p, c := openHome(t, toolCard("card-0", "make lint"), toolCard("card-1", "go test ./..."))
	Eval[bool](p, homeSafe+`.focus() || true`)
	p.Key(kb.Escape)
	p.WaitJS(`document.querySelectorAll(".home-waiting .card-inline").length === 1`)
	noAnswer(t, c, 300*time.Millisecond)
	Eval[bool](p, homeSafe+`.focus() || true`)
	p.Key("\r")
	expectAnswer(t, c, "deny")
}

// #/home?card=<id> opens the sheet over the Home with the safe answer
// focused; Esc closes it back to #/home and answers nothing.
func TestHomeCardQueryOpensTheSheet(t *testing.T) {
	p, c := openHome(t, toolCard("card-1", "go test ./..."))
	Eval[bool](p, `(location.hash = "#/home?card=card-1", true)`)
	p.WaitJS(dialog + ` && ` + dialog + `.contains(document.activeElement)`)
	p.WaitJS(`document.querySelector(".home-waiting")`)
	p.Key(kb.Escape)
	p.WaitJS(`!` + dialog + ` && location.hash === "#/home"`)
	noAnswer(t, c, 300*time.Millisecond)
}

// A review card with a diff is never accepted unseen from the stream: its
// inline card offers Reject and "Open Changes", which opens the sheet
// with the diff.
func TestHomeInlineReviewOpensTheSheet(t *testing.T) {
	p, _ := openHome(t, kindCard("card-1", "review"))
	got := Eval[[]string](p, `[...document.querySelectorAll(".home-waiting .card-inline .card-foot button")].map((b) => b.textContent.trim())`)
	if len(got) != 2 || got[0] != "Reject" || got[1] != "Open Changes" {
		t.Fatalf("inline review buttons %q, want Reject, Open Changes", got)
	}
	p.TapJS(`[...document.querySelectorAll(".home-waiting .card-inline .card-foot button")][1]`)
	p.WaitJS(dialog + ` && location.hash === "#/home?card=card-1" && document.querySelector(".sheet .card-diff")`)
}

// A card the laptop answered first leaves the stream; a trust card stays
// with Later / Answer on laptop and no answer button.
func TestHomeInlineCardsFollowTheTopic(t *testing.T) {
	p, c := openHome(t, toolCard("card-1", "go test ./..."), trustCard("card-2"))
	if n := Eval[int](p, `document.querySelectorAll(".home-waiting .card-inline").length`); n != 2 {
		t.Fatalf("%d inline cards, want 2", n)
	}
	if txt := p.Text(".home-waiting .card-trust .card-foot"); strings.Contains(txt, "Allow") {
		t.Errorf("trust card offers %q", txt)
	}
	c.Send(t, Delta("cards", 2, map[string]any{"op": "remove", "id": "card-1"}))
	p.WaitJS(`document.querySelectorAll(".home-waiting .card-inline").length === 1`)
}

// The Home with inline cards passes the accessibility checks in both themes.
func TestAxeHomeCards(t *testing.T) {
	src := axeSource(t)
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			p, _ := openHome(t, toolCard("card-1", "go test ./..."), kindCard("card-2", "review"))
			p.Media(map[string]string{"prefers-color-scheme": theme, "prefers-reduced-motion": "reduce"})
			p.axe(src, "home cards")
		})
	}
}

// noAnswer checks that the page sends no cards.answer for d; the Home's
// own subscriptions and reads on the way are fine.
func noAnswer(t *testing.T, c *HubConn, d time.Duration) {
	t.Helper()
	end := time.After(d)
	for {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && r.Op == "cards.answer" {
				t.Fatalf("page sent %s; want no answer", show(r))
			}
		case <-end:
			return
		}
	}
}
