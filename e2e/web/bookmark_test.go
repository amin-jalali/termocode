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
