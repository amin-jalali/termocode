//go:build unix

package webgw

import (
	"context"
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// phoneWatchRig is a WS rig with a paired-device store and fast pings;
// uses counts the gateway's idle-timer marks (Activity).
func phoneWatchRig(t *testing.T, watch bool) (*wsRig, *webdevices.Store, *atomic.Int64) {
	t.Helper()
	store := webdevices.Open(t.TempDir())
	var uses atomic.Int64
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) {
		c.Devices, c.PhoneWatch = store, watch
		c.PingEvery, c.PongWait = 30*time.Millisecond, 2*time.Second
		c.Activity = func() { uses.Add(1) }
	})
	return r, store, &uses
}

// pairedToken adds a device to the store and issues a session for it.
func pairedToken(t *testing.T, r *wsRig, store *webdevices.Store, id string, guest bool) string {
	t.Helper()
	if err := store.Add(webdevices.Device{ID: id, Name: "Phone", Role: webdevices.RoleViewer, Guest: guest,
		Created: time.Unix(1700000000, 0)}); err != nil {
		t.Fatal(err)
	}
	tok, _, err := r.tokens.Issue(id, RoleViewer, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// sessLastSeen reads a session's LastSeen without touching it.
func sessLastSeen(t *Tokens, tok string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.byHash[sha256.Sum256([]byte(tok))]; ok {
		return s.LastSeen
	}
	return time.Time{}
}

// quietBrowser reads (so pongs go back) but never sends: a phone whose
// page is hidden or whose screen is off.
func quietBrowser(c *websocket.Conn) {
	go func() {
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}()
}

// usesAfter waits a few ping rounds and returns the uses counted then.
func usesAfter(uses *atomic.Int64) int64 {
	time.Sleep(300 * time.Millisecond) // ~10 ping rounds
	return uses.Load()
}

// Spec (MOB-31): with web_phone "on", a paired phone that only watches
// (its link answers the gateway's pings, it sends nothing) keeps the
// gateway up: each answered ping is a use for the idle timer.
func TestPhoneWatchKeepsGatewayUp(t *testing.T) {
	r, store, uses := phoneWatchRig(t, true)
	c, _ := r.connectWith(pairedToken(t, r, store, "dev-phone1", false))
	quietBrowser(c)
	start := uses.Load()
	waitFor(t, "answered pings counted as use", func() bool { return uses.Load() >= start+3 })
}

// Spec (MOB-31): the session of a watching phone is kept fresh too, so
// the gateway's own token idle time does not drop it while it watches.
func TestPhoneWatchRefreshesSession(t *testing.T) {
	r, store, _ := phoneWatchRig(t, true)
	tok := pairedToken(t, r, store, "dev-phone2", false)
	c, _ := r.connectWith(tok)
	quietBrowser(c)
	before, err := r.tokens.Check(tok, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	r.clock.mu.Lock()
	r.clock.now = r.clock.now.Add(time.Minute)
	r.clock.mu.Unlock()
	waitFor(t, "session seen again", func() bool {
		return sessLastSeen(r.tokens, tok).After(before.LastSeen)
	})
}

// Spec (MOB-31, safe choice first): web_phone off (the default) keeps
// today's rule: a quiet link is no use, so the idle timer runs.
func TestPhoneWatchOffQuietIsIdle(t *testing.T) {
	r, store, uses := phoneWatchRig(t, false)
	c, _ := r.connectWith(pairedToken(t, r, store, "dev-phone3", false))
	quietBrowser(c)
	start := uses.Load()
	if n := usesAfter(uses); n != start {
		t.Fatalf("web_phone off: %d uses from a quiet link", n-start)
	}
}

// Spec (MOB-31): only the owner's own paired devices watch. A guest
// device (WEB-50) and the one-time launch link ("loc-…") never keep the
// gateway up by being open.
func TestPhoneWatchNotGuestNotLaunchLink(t *testing.T) {
	r, store, uses := phoneWatchRig(t, true)
	g, _ := r.connectWith(pairedToken(t, r, store, "dev-guest1", true))
	quietBrowser(g)
	l, _ := r.connect() // a launch-link session
	quietBrowser(l)
	start := uses.Load()
	if n := usesAfter(uses); n != start {
		t.Fatalf("%d uses from a guest or the launch link", n-start)
	}
}

// Spec (MOB-31): watching never passes the max lifetime: a watching
// phone's session past web_max_hours ends (4002) like any other.
func TestPhoneWatchStopsAtMax(t *testing.T) {
	r, store, _ := phoneWatchRig(t, true)
	c, _ := r.connectWith(pairedToken(t, r, store, "dev-phone4", false))
	r.clock.mu.Lock()
	r.clock.now = r.clock.now.Add(DefaultMax + time.Minute)
	r.clock.mu.Unlock()
	if code := closeCode(t, c); code != 4002 {
		t.Fatalf("close code %d, want 4002", code)
	}
}
