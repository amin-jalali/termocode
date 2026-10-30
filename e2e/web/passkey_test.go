//go:build browser

package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webgw"
)

// localhostBase is the page origin passkeys work on: an RP id may not be
// an IP address, and localhost is a secure context without TLS.
func (s *Server) localhostBase() string {
	return "http://localhost:" + strings.TrimPrefix(s.Base, "http://127.0.0.1:")
}

// virtualAuthenticator gives the tab a platform authenticator that keeps
// discoverable keys and verifies the user (Chrome DevTools WebAuthn).
func (p *Page) virtualAuthenticator() {
	p.T.Helper()
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(ctx, t, webauthn.Enable, webauthn.EnableParams{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, t, webauthn.AddVirtualAuthenticator, webauthn.AddVirtualAuthenticatorParams{
			Options: &webauthn.VirtualAuthenticatorOptions{
				Protocol: webauthn.AuthenticatorProtocolCtap2, Transport: webauthn.AuthenticatorTransportInternal,
				HasResidentKey: true, HasUserVerification: true, IsUserVerified: true, AutomaticPresenceSimulation: true,
			}})
		return err
	}))
}

// Spec (WEB-32/33): the whole pairing in a real browser. The QR link's
// secret leaves the URL at once; one tap shows the code the laptop's card
// shows; after the owner's yes a second tap saves a passkey and the page
// opens the workspace with the issued token. Later, with no session in
// the tab, the same device signs in with its passkey alone.
func TestPasskeyPairAndSignInInTheBrowser(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, err := webgw.NewLinkSecret()
	if err != nil {
		t.Fatal(err)
	}
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.virtualAuthenticator()
	p.WaitJS(`document.querySelector(".pair .connect-foot button")`)
	if h := Eval[string](p, `location.href`); strings.Contains(h, plain) {
		t.Fatalf("the pairing secret is still in the URL: %s", h)
	}
	p.Tap(".pair .connect-foot button")
	var ask = <-s.Asks
	p.WaitJS(`document.querySelector(".pair-code")`)
	if got := strings.ReplaceAll(p.Text(".pair-code"), " ", ""); got != ask.Code {
		t.Fatalf("the page shows %q, the laptop's card %q", got, ask.Code)
	}
	if !strings.Contains(ask.Name, " · ") {
		t.Errorf("device name %q", ask.Name)
	}
	s.PairAnswers <- webdevices.RoleControl
	p.WaitJS(`[...document.querySelectorAll(".pair .connect-foot button")].some((b) => b.textContent.trim() === "Save passkey")`)
	p.Tap(".pair .connect-foot button")
	c := s.Hub.Accept(t)
	c.HandshakeMeta(t, Meta("c-paired"))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	ds, _ := s.Devices.List()
	if len(ds) != 1 || len(ds[0].PublicKey) == 0 || ds[0].Role != webdevices.RoleControl {
		t.Fatalf("devices %+v", ds)
	}

	// A new session in this tab: the sign-in screen, then the passkey.
	Eval[bool](p, `(sessionStorage.clear(), true)`)
	p.Do(chromedp.Reload())
	p.WaitJS(`document.querySelector(".connect-foot button")?.textContent.trim() === "Sign in with passkey"`)
	p.Tap(".connect-foot button")
	c2 := s.Hub.Accept(t)
	hello := c2.HandshakeMeta(t, Meta("c-again"))
	sess, err := s.Tokens.Check(hello.Token, time.Now())
	if err != nil || sess.Device != ds[0].ID {
		t.Fatalf("signed in as %+v %v", sess, err)
	}
	if after, _, _ := s.Devices.Get(ds[0].ID); after.SignCount == 0 && after.LastSeen.IsZero() {
		t.Errorf("the sign-in was not recorded: %+v", after)
	}
}

// Spec (WEB-32): Not now on the laptop: the page says so and saves no
// passkey; nothing is stored.
func TestPairNotNowInTheBrowser(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, _ := webgw.NewLinkSecret()
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.Tap(".pair .connect-foot button")
	<-s.Asks
	s.PairAnswers <- ""
	p.WaitJS(`document.querySelector(".pair .connect-line.is-error")?.textContent.includes("Not now")`)
	if Eval[bool](p, `!!document.querySelector(".pair .connect-foot button")`) {
		t.Error("a button is left after Not now")
	}
	if ds, _ := s.Devices.List(); len(ds) != 0 {
		t.Fatalf("stored %+v", ds)
	}
}
