//go:build browser

package web

import (
	"context"
	"strings"
	"testing"

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
