//go:build browser

package web

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// When the link dies the page goes offline, keeps what it shows, waits
// 0.5, 1, 2, 4 s (±20 %) between tries, and comes back with a resume of
// the same connection. Nothing is queued while offline.
func TestLinkReconnectsWithBackoffAndResumes(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			s.Hub.Refuse(true)
			before := len(s.Hub.Attempts())
			c.Drop()
			down := time.Now()
			p.WaitJS(`document.documentElement.dataset.link === "offline"`)
			// The strip keeps the last status while offline (web UX P5).
			if got := p.Text(".strip .branch"); !strings.Contains(got, "main") {
				t.Errorf("strip lost the branch while offline: %q", got)
			}
			p.WaitJS(`document.querySelector(".bar-lost")`)

			// Four refused tries: 0.5, 1, 2 and 4 s apart.
			deadline := time.Now().Add(20 * time.Second)
			for len(s.Hub.Attempts())-before < 4 {
				if time.Now().After(deadline) {
					t.Fatalf("only %d tries in 20 s", len(s.Hub.Attempts())-before)
				}
				time.Sleep(50 * time.Millisecond)
			}
			at := s.Hub.Attempts()[before:]
			prev := down
			for i, base := range []time.Duration{500, 1000, 2000, 4000} {
				base *= time.Millisecond
				gap := at[i].Sub(prev)
				lo, hi := base*8/10-50*time.Millisecond, base*12/10+time.Second
				if gap < lo || gap > hi {
					t.Errorf("try %d came %v after the last; want %v..%v", i+1, gap, lo, hi)
				}
				prev = at[i]
			}

			// Try now skips the wait (the next one would be 8 s).
			s.Hub.Refuse(false)
			p.Tap(".bar-lost button")
			c2 := s.Hub.Accept(t)
			hello := c2.Handshake(t, "c-1")
			if hello.Resume == nil || hello.Resume.Conn != "c-1" {
				t.Fatalf("hello.resume = %+v, want conn c-1", hello.Resume)
			}
			p.WaitJS(`document.documentElement.dataset.link === "online"`)
			p.WaitJS(`document.querySelector(".bar-back")`)
			for _, m := range append(c.Sent(), c2.Sent()...) {
				if _, ok := m.(*webproto.Req); ok {
					t.Errorf("a request went out: %s", show(m))
				}
			}
		})
	}
}

// Close codes that retrying cannot fix end the session: the page says
// why in the final bar and never knocks again. 4001 also drops the token.
func TestLinkFinalCloseStopsForGood(t *testing.T) {
	cases := []struct {
		code      webproto.CloseCode
		text      string
		dropToken bool
	}{
		{webproto.CloseStopped, "The web session was stopped.", false},
		{webproto.CloseIdle, "15 minutes", false},
		{webproto.CloseAuthFailed, "This link has expired", true},
		{webproto.CloseUnsupportedVersion, "Reload", false},
	}
	for _, tc := range cases {
		t.Run(tc.code.Reason(), func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Phone)
			c := p.Login("c-1")
			c.Close(t, tc.code)
			p.WaitJS(`document.documentElement.dataset.link === "final"`)
			p.WaitJS(`document.querySelector(".bar-final")`)
			if got := p.Text(".bar-final"); !strings.Contains(got, tc.text) {
				t.Errorf("final bar %q, want %q in it", got, tc.text)
			}
			s.Hub.NoConn(t, 1500*time.Millisecond) // three backoff steps' worth
			kept := Eval[bool](p, `sessionStorage.getItem("termocode.session") !== null`)
			if kept == tc.dropToken {
				t.Errorf("token kept = %v after %d", kept, tc.code)
			}
		})
	}
}

// A reload keeps the tab's session: the token in sessionStorage opens a
// new connection without a new link (TestReloadResumes, web design 5.3).
func TestReloadKeepsTheSession(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	p.Do(chromedp.Reload())
	c2 := s.Hub.Accept(t)
	c2.Handshake(t, "c-2")
	c2.Subs(t, "status")
	c2.Send(t, Snap("status", 1, statusData("after-reload", 0)))
	p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("after-reload")`)
	<-c.Done()
}

// A new tab has no token: it needs a new link and shows the "open from
// the laptop" line, never the app (TestNewTabNeedsLink).
func TestNewTabNeedsLink(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Do(chromedp.Navigate(s.Path("#/chat")))
	p.WaitJS(`document.querySelector(".connect-line.is-error")`)
	if got := p.Text(".connect-line"); !strings.Contains(got, "Open this page from termocode") {
		t.Errorf("connect line %q", got)
	}
	s.Hub.NoConn(t, 500*time.Millisecond)
}

// The idle text says the panel's own idle limit from meta (the laptop's
// web_idle_minutes), never a fixed number; without one it stays general.
func TestLinkIdleCloseSaysTheRealLimit(t *testing.T) {
	for _, tc := range []struct {
		idleS     int
		want, not string
	}{
		{1800, "after 30 minutes without use", "15"},
		{60, "after 1 minute without use", "15"},
		{0, "stopped after a time without use", "minutes"},
	} {
		t.Run(strconv.Itoa(tc.idleS), func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Phone)
			meta := Meta("c-1")
			meta.IdleS = tc.idleS
			c := p.LoginMeta(meta)
			c.Close(t, webproto.CloseIdle)
			p.WaitJS(`document.querySelector(".bar-final")`)
			if got := p.Text(".bar-final"); !strings.Contains(got, tc.want) || strings.Contains(got, tc.not) {
				t.Errorf("final bar %q, want %q and no %q", got, tc.want, tc.not)
			}
		})
	}
}

// The max-time text says the panel's own limit from meta too.
func TestLinkMaxCloseSaysTheRealLimit(t *testing.T) {
	for _, tc := range []struct {
		maxS      int
		want, not string
	}{
		{4 * 3600, "after 4 hours", "12"},
		{3600, "after 1 hour.", "12"},
		{0, "stopped after its time limit", "hours"},
	} {
		t.Run(strconv.Itoa(tc.maxS), func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Phone)
			meta := Meta("c-1")
			meta.MaxS = tc.maxS
			c := p.LoginMeta(meta)
			c.Close(t, webproto.CloseMaxLifetime)
			p.WaitJS(`document.querySelector(".bar-final")`)
			if got := p.Text(".bar-final"); !strings.Contains(got, tc.want) || strings.Contains(got, tc.not) {
				t.Errorf("final bar %q, want %q and no %q", got, tc.want, tc.not)
			}
		})
	}
}
