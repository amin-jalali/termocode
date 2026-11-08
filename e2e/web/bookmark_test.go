//go:build browser

package web

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webgw"
)

// noKeyInStorage fails when the page's JavaScript can see a bookmark key:
// in localStorage, sessionStorage or document.cookie.
func (p *Page) noKeyInStorage(where string) {
	p.T.Helper()
	if got := Eval[string](p, `(() => {
		const all = [];
		for (const s of [localStorage, sessionStorage]) for (let i = 0; i < s.length; i++) all.push(s.key(i) + "=" + s.getItem(s.key(i)));
		all.push(document.cookie);
		return all.filter((v) => v.includes("tcb1.") || v.startsWith("termocode.device=")).join(" | ");
	})()`); got != "" {
		p.T.Fatalf("%s: page code can read a bookmark key: %s", where, got)
	}
}

// deviceCookie is the browser's bookmark cookie for the sign-in path,
// read over DevTools (page code cannot), or nil.
func (p *Page) deviceCookie(base string) *network.Cookie {
	p.T.Helper()
	var got *network.Cookie
	p.Do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, network.GetCookies, network.GetCookiesParams{URLs: []string{base + "/login/device"}})
		for _, c := range res.Cookies {
			if c.Name == webgw.DeviceCookie {
				got = c
			}
		}
		return err
	}))
	return got
}

// Spec (WEBP-12, security): Remember this browser keeps the bookmark key
// in an httpOnly, SameSite=Strict cookie the laptop sets, never in
// localStorage or anywhere page code can read it. A bookmark then still
// opens the workspace with no passkey, and the key turns over.
func TestBookmarkKeyNotInLocalStorage(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, _ := webgw.NewLinkSecret()
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.virtualAuthenticator()
	p.Tap(".pair .connect-foot button")
	<-s.Asks
	s.PairAnswers <- webdevices.RoleControl
	p.WaitJS(`!!document.querySelector(".pair .remember input")`)
	p.Tap(".pair .remember input")
	p.WaitJS(`document.querySelector(".pair .remember input").checked`)
	p.Tap(".pair .connect-foot .btn-primary")
	c := s.Hub.Accept(t)
	c.HandshakeMeta(t, Meta("c-paired"))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.noKeyInStorage("after pairing")
	ck := p.deviceCookie(s.localhostBase())
	if ck == nil || !ck.HTTPOnly || ck.SameSite != network.CookieSameSiteStrict || ck.Path != "/login" {
		t.Fatalf("bookmark cookie %+v", ck)
	}
	first := ck.Value

	// A bookmark, later: no session in the tab, the cookie alone signs in.
	Eval[bool](p, `(sessionStorage.clear(), true)`)
	p.Reload()
	c2 := s.Hub.Accept(t)
	hello := c2.HandshakeMeta(t, Meta("c-bookmark"))
	if _, err := s.Tokens.Check(hello.Token, time.Now()); err != nil {
		t.Fatalf("bookmark sign-in: %v", err)
	}
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.noKeyInStorage("after the bookmark sign-in")
	if ck := p.deviceCookie(s.localhostBase()); ck == nil || ck.Value == first {
		t.Fatalf("the key did not turn over: %+v", ck)
	}
}

// Spec (WEBP-12): a browser remembered before the cookie still keeps its
// key in localStorage. On open the page sends it once, the laptop moves
// it into the cookie, and the localStorage copy is deleted.
func TestBookmarkOldLocalStorageKeyMovesToTheCookie(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	pairInBrowser(t, s, p, webdevices.RoleControl, Meta("c-paired"))
	ds, _ := s.Devices.List()
	key, hash, err := webgw.NewDeviceKey(ds[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Devices.Change(ds[0].ID, func(d *webdevices.Device) {
		d.Bookmark = &webdevices.Bookmark{Hash: hash, Origin: s.localhostBase(), Created: now, Used: now}
	}); err != nil {
		t.Fatal(err)
	}
	Eval[bool](p, `(sessionStorage.clear(), localStorage.setItem("termocode.device", `+strconv.Quote(key)+`), true)`)
	p.Reload()
	c := s.Hub.Accept(t)
	c.HandshakeMeta(t, Meta("c-moved"))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.noKeyInStorage("after the move")
	if ck := p.deviceCookie(s.localhostBase()); ck == nil || !ck.HTTPOnly || ck.Value == key {
		t.Fatalf("the old key was not moved into a new cookie: %+v", ck)
	}
}

// Spec (MOB stabilize, tablet 2026-11-07): a remembered browser whose
// device the laptop removed says so when it opens again, "This device was
// removed. Pair it again from termocode.", not "Open this page from
// termocode" (which sent the tablet to a link it cannot get).
func TestBookmarkOfARemovedDeviceSaysRemoved(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, _ := webgw.NewLinkSecret()
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.virtualAuthenticator()
	p.Tap(".pair .connect-foot button")
	<-s.Asks
	s.PairAnswers <- webdevices.RoleControl
	p.WaitJS(`!!document.querySelector(".pair .remember input")`)
	p.Tap(".pair .remember input")
	p.WaitJS(`document.querySelector(".pair .remember input").checked`)
	p.Tap(".pair .connect-foot .btn-primary")
	c := s.Hub.Accept(t)
	c.HandshakeMeta(t, Meta("c-paired"))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)

	devs, err := s.Devices.List()
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices: %v %v", devs, err)
	}
	if _, ok, err := s.Devices.Remove(devs[0].ID); !ok || err != nil {
		t.Fatalf("remove: %v %v", ok, err)
	}
	Eval[bool](p, `(sessionStorage.clear(), true)`)
	p.Reload()
	p.WaitJS(`(document.querySelector(".connect-line.is-error")?.textContent ?? "").includes("This device was removed")`)
	if got := Eval[string](p, `document.querySelector(".connect-line")?.textContent ?? ""`); got != "This device was removed. Pair it again from termocode." {
		t.Fatalf("connect line = %q", got)
	}
}

// Spec (MOB stabilize, tablet 2026-11-08): the page marks a sign-in that
// cannot go on, so the phone app can show its own Pair again screen (the
// page has no way back to the app's Connect screen): <html data-signin>
// is "removed" when the laptop refused the bookmark as removed, and
// "none" when the page has nothing to sign in with. It is gone once the
// workspace is open.
func TestSignInDeadEndIsMarkedForTheApp(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, _ := webgw.NewLinkSecret()
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.virtualAuthenticator()
	p.Tap(".pair .connect-foot button")
	<-s.Asks
	s.PairAnswers <- webdevices.RoleControl
	p.WaitJS(`!!document.querySelector(".pair .remember input")`)
	p.Tap(".pair .remember input")
	p.WaitJS(`document.querySelector(".pair .remember input").checked`)
	p.Tap(".pair .connect-foot .btn-primary")
	c := s.Hub.Accept(t)
	c.HandshakeMeta(t, Meta("c-paired"))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	if got := Eval[string](p, `document.documentElement.dataset.signin ?? ""`); got != "" {
		t.Fatalf("open workspace: data-signin = %q, want none", got)
	}

	devs, err := s.Devices.List()
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices: %v %v", devs, err)
	}
	if _, ok, err := s.Devices.Remove(devs[0].ID); !ok || err != nil {
		t.Fatalf("remove: %v %v", ok, err)
	}
	Eval[bool](p, `(sessionStorage.clear(), true)`)
	p.Reload()
	p.WaitJS(`document.documentElement.dataset.signin === "removed"`)

	// The laptop cleared the cookie: the next open has nothing to go on
	// (an address, where a passkey cannot be used, like a Wi-Fi laptop).
	p.Do(chromedp.Navigate(s.Base + "/"))
	p.WaitJS(`document.documentElement.dataset.signin === "none"`)
}

// Spec (MOB stabilize, tablet 2026-11-08): the Remember this browser hint
// is true on a Wi-Fi laptop too, where there is no passkey: it never
// says "without the passkey".
func TestRememberHintSaysWhatHappensWithNoPasskey(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	plain, digest, _ := webgw.NewLinkSecret()
	s.Pairing.SetSecret(digest, webgw.PairTTL, false)
	p.Do(chromedp.Navigate(s.localhostBase() + "/#p=" + plain))
	p.virtualAuthenticator()
	p.Tap(".pair .connect-foot button")
	<-s.Asks
	s.PairAnswers <- webdevices.RoleControl
	p.WaitJS(`!!document.querySelector(".pair .remember-hint")`)
	if got := Eval[string](p, `document.querySelector(".pair .remember-hint").textContent`); got != "Next time, this browser signs back in by itself. Only this browser, only this address." {
		t.Fatalf("remember hint = %q", got)
	}
}
