//go:build browser

package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The launch secret leaves the URL before anything else happens: before
// the user taps Open (so before /auth), and in a way that leaves no
// history entry holding it (web design 3.1, T10).
func TestFragmentClearedBeforeAuth(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			secret := p.Open()
			if href := Eval[string](p, "location.href"); strings.Contains(href, secret) {
				t.Fatalf("the URL still holds the secret: %s", href)
			}
			if h := Eval[string](p, "location.hash"); h != "#/chat" {
				t.Errorf("hash after the secret is taken = %q, want #/chat", h)
			}
			for _, u := range p.Requests() {
				if strings.HasSuffix(u, "/auth") {
					t.Fatalf("the page called /auth before the user tapped Open")
				}
			}
			hist := p.history()
			for _, u := range hist {
				if strings.Contains(u, secret) {
					t.Fatalf("a history entry holds the secret: %s", u)
				}
			}
			p.Tap(".connect-foot button")
			c := s.Hub.Accept(t)
			c.Handshake(t, "c-1")
			for _, u := range append(p.Requests(), p.history()...) {
				if strings.Contains(u, secret) {
					t.Errorf("the secret shows up in %s", u)
				}
			}
			kept := Eval[string](p, `JSON.stringify([sessionStorage, localStorage, document.cookie])`)
			if strings.Contains(kept, secret) {
				t.Errorf("the secret was stored: %s", kept)
			}
		})
	}
}

// history returns the URLs of the tab's navigation history.
func (p *Page) history() []string {
	p.T.Helper()
	var out []string
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		h, err := chromedp.NavigationEntries()(ctx, tg)
		if err != nil {
			return err
		}
		for _, e := range h.Entries {
			out = append(out, e.URL)
		}
		return nil
	}))
	return out
}

// tourAll visits every tab, flips the theme and the text direction on
// More and comes back: the page's whole surface today.
func tourAll(p *Page) {
	p.T.Helper()
	for _, tab := range []string{"files", "term", "git", "more", "chat"} {
		p.GoTab(tab)
	}
	p.GoTab("more")
	p.Tap(`.more .field:nth-of-type(1) .seg-item:nth-child(3)`) // light
	p.WaitJS(`document.documentElement.dataset.theme === "light"`)
	p.Tap(`.more .field:nth-of-type(3) .seg-item:nth-child(3)`) // right to left
	p.WaitJS(`document.documentElement.dir === "rtl"`)
	p.GoTab("chat")
}

// The page runs under its strict CSP (Trusted Types, no inline code or
// style) without a single violation, error or warning.
func TestCSPNoViolations(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Login("c-1")
			tourAll(p)
			// Problems arrive as events; give the last ones a moment.
			p.Holds("true", 300*time.Millisecond)
			if got := p.Problems(); len(got) > 0 {
				t.Fatalf("the page reported problems:\n%s", strings.Join(got, "\n"))
			}
		})
	}
}

// The detector itself: an inline style attribute breaks the CSP and is
// reported. Without this, TestCSPNoViolations could pass by seeing nothing.
func TestCSPViolationIsReported(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Open()
	Eval[bool](p, `(document.body.setAttribute("style", "color: red"), true)`)
	deadline := time.Now().Add(wait)
	for {
		for _, pr := range p.Problems() {
			if strings.Contains(pr, "style") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("an inline style was not reported; problems: %v", p.Problems())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Nothing is fetched from anywhere but the gateway: no CDN, no fonts, no
// analytics; the only WebSocket is the page's own /ws.
func TestNoExternalRequests(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Login("c-1")
			tourAll(p)
			host := strings.TrimPrefix(s.Base, "http://")
			own := map[string]bool{}
			for _, u := range p.Requests() {
				switch {
				case strings.HasPrefix(u, s.Base+"/"):
					own[strings.TrimPrefix(u, s.Base)] = true
				case u == "ws://"+host+"/ws":
					own["/ws"] = true
				case strings.HasPrefix(u, "data:"):
				default:
					t.Errorf("request to %s", u)
				}
			}
			for _, want := range []string{"/", "/assets/app.js", "/assets/app.css", "/auth", "/ws"} {
				if !own[want] {
					t.Errorf("no request for %s seen (seen: %v); is the recorder working?", want, keys(own))
				}
			}
		})
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
