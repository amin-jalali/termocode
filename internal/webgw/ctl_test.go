//go:build unix

package webgw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func waitCtl(t *testing.T, c *Ctl) webproto.CloseCode {
	t.Helper()
	select {
	case <-c.Done():
		return c.Code()
	case <-time.After(10 * time.Second):
		t.Fatal("control connection did not end")
	}
	return 0
}

// Spec (WEB-8): the control connection says {"t":"ctl"} first; the hub's
// bye 4009 (kill switch) ends it with that code; other lines are ignored.
func TestCtlByeIsTheKillSwitch(t *testing.T) {
	hub := newFakeHub(t)
	c, err := DialCtl(context.Background(), hub.dial, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := hub.next()
	if m := p.line(t); len(m) != 1 || m["t"] != "ctl" {
		t.Fatalf("first line %v", m)
	}
	p.send(t, `{"t":"pong","n":"1"}`)
	p.send(t, `{"t":"bye","code":4009}`)
	if got := waitCtl(t, c); got != webproto.CloseStopped {
		t.Fatalf("code %d, want 4009", got)
	}
	p.eof(t)
}

// Spec: the hub going away ends the control connection with 4012; no hub
// at all is an error at the start.
func TestCtlHubGone(t *testing.T) {
	hub := newFakeHub(t)
	c, err := DialCtl(context.Background(), hub.dial, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := hub.next()
	_ = p.nc.Close()
	if got := waitCtl(t, c); got != webproto.CloseHubGone {
		t.Fatalf("code %d, want 4012", got)
	}
	hub.setDown()
	if _, err := DialCtl(context.Background(), hub.dial, nil, nil); err == nil {
		t.Fatal("no error without a hub")
	}
}

// Spec (WEB-9): a link event hands the gateway a new secret digest; a
// bad digest is ignored.
func TestCtlLinkEvent(t *testing.T) {
	hub := newFakeHub(t)
	got := make(chan [sha256.Size]byte, 2)
	c, err := DialCtl(context.Background(), hub.dial, func(d [sha256.Size]byte) { got <- d }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := hub.next()
	_, digest, _ := NewLinkSecret()
	p.send(t, `{"t":"ev","kind":"link","data":{"sha256":"abc"}}`)
	p.send(t, `{"t":"ev","kind":"link","data":{"sha256":"`+hex.EncodeToString(digest[:])+`"}}`)
	select {
	case d := <-got:
		if d != digest {
			t.Fatal("wrong digest")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no link")
	}
	select {
	case <-got:
		t.Fatal("bad digest passed")
	default:
	}
}
