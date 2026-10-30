//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-32): the gateway asks the laptop on its control connection
// (a req with a rising id) and waits for that id's answer; answers out of
// order reach the right caller; an err is a *CtlError with its code.
func TestCtlCall(t *testing.T) {
	hub := newFakeHub(t)
	c, err := DialCtl(context.Background(), hub.dial, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := hub.next()
	p.line(t) // ctl
	type res struct {
		data json.RawMessage
		err  error
	}
	a, b := make(chan res, 1), make(chan res, 1)
	go func() {
		d, err := c.Call(context.Background(), webproto.OpWebTotpCheck, webproto.TotpCheckArgs{Code: "123456"})
		a <- res{d, err}
	}()
	first := p.line(t)
	go func() {
		d, err := c.Call(context.Background(), webproto.OpWebGrantAsk, webproto.GrantAskArgs{Device: "dev-1", Name: "Phone"})
		b <- res{d, err}
	}()
	second := p.line(t)
	if first["t"] != "req" || first["op"] != "web.totp_check" || second["op"] != "web.grant_ask" || second["id"].(float64) <= first["id"].(float64) {
		t.Fatalf("requests %v %v", first, second)
	}
	p.send(t, `{"t":"err","id":`+jsonNum(second["id"])+`,"code":"forbidden","msg":"Not now."}`)
	p.send(t, `{"t":"res","id":`+jsonNum(first["id"])+`,"data":{"ok":true}}`)
	select {
	case r := <-a:
		if r.err != nil || string(r.data) != `{"ok":true}` {
			t.Fatalf("first: %s %v", r.data, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no answer")
	}
	r := <-b
	var ce *CtlError
	if !errors.As(r.err, &ce) || ce.Code != webproto.CodeForbidden {
		t.Fatalf("second: %v", r.err)
	}
}

// A call ends with its context, and every waiting call fails once the
// hub is gone: nothing waits for ever.
func TestCtlCallEnds(t *testing.T) {
	hub := newFakeHub(t)
	c, err := DialCtl(context.Background(), hub.dial, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := hub.next()
	p.line(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, webproto.OpWebTotpCheck, webproto.TotpCheckArgs{Code: "123456"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), webproto.OpWebTotpCheck, webproto.TotpCheckArgs{Code: "123456"})
		done <- err
	}()
	p.line(t)
	p.line(t)
	_ = p.nc.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrCtlGone) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("call still waits after the hub went away")
	}
}

// Spec (WEB-32): events other than link and audit (pair, revoke, role)
// reach the gateway's event handler with their data.
func TestCtlEvents(t *testing.T) {
	hub := newFakeHub(t)
	got := make(chan string, 2)
	c, err := DialCtlWith(context.Background(), hub.dial, CtlHandlers{Event: func(kind string, data json.RawMessage) {
		got <- kind + " " + string(data)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := hub.next()
	p.line(t)
	p.send(t, `{"t":"ev","kind":"revoke","data":{"device":"dev-1"}}`)
	if g := <-got; g != `revoke {"device":"dev-1"}` {
		t.Fatalf("event %q", g)
	}
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
