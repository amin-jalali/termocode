//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webHello opens a browser connection and reads its meta.
func (r *webRig) webHello(open string) (*webClient, map[string]any) {
	r.t.Helper()
	c := r.dial(open)
	c.send(`{"t":"hello","v":2,"token":"tok-` + `secret-value"}`)
	meta := r.nextMsg(c)
	if meta["t"] != "meta" {
		r.t.Fatalf("first answer %v, want meta", meta)
	}
	return c, meta
}

// Spec (WEB-6): hello is answered with meta built on Update: the
// connection id, role and device from the open line, the role's caps,
// the number of browsers and the laptop's theme. The token never comes
// back.
func TestHubHelloMeta(t *testing.T) {
	r := newWebRig(t, nil)
	r.m.themeID = "test-theme"
	c, meta := r.webHello(webTestOpen)
	if meta["conn"] != "c-1" || meta["role"] != "controller" || meta["device"] != "loc-1" ||
		meta["v"] != float64(webproto.Version) || meta["viewers"] != float64(1) {
		t.Fatalf("meta %v", meta)
	}
	if meta["idle_s"] != float64(15*60) || meta["max_s"] != float64(12*60*60) {
		t.Fatalf("meta limits %v", meta)
	}
	if ui, _ := meta["ui"].(map[string]any); ui["theme"] != "test-theme" {
		t.Fatalf("meta ui %v", meta["ui"])
	}
	caps, _ := meta["caps"].([]any)
	want := webproto.CapsFor(webproto.RoleController)
	if len(caps) != len(want) {
		t.Fatalf("caps %v, want %v", caps, want)
	}
	c.send(`{"t":"ping","n":"7"}`)
	if pong := r.nextMsg(c); pong["t"] != "pong" || pong["n"] != "7" {
		t.Fatalf("pong %v", pong)
	}
	c2, meta2 := r.webHello(`{"t":"open","conn":"c-2","role":"viewer","device":"loc-2"}`)
	if meta2["viewers"] != float64(2) || meta2["role"] != "viewer" {
		t.Fatalf("second meta %v", meta2)
	}
	if caps2, _ := meta2["caps"].([]any); len(caps2) != len(webproto.CapsFor(webproto.RoleViewer)) {
		t.Fatalf("viewer caps %v", caps2)
	}
	_ = c2
}

// Spec (WEB-6): protocol errors end the connection after one err line: a
// message before hello, a second hello, a bad message, a hello on the
// control connection. bye ends it quietly.
func TestHubProtocolErrors(t *testing.T) {
	r := newWebRig(t, nil)
	for name, lines := range map[string][]string{
		"before hello": {`{"t":"ping","n":"1"}`},
		"two hellos":   {`{"t":"hello","v":2,"token":"x"}`, `{"t":"hello","v":2,"token":"x"}`},
		"bad message":  {`{"t":"hello","v":2,"token":"x"}`, `{"t":"ping","n":"1","x":1}`},
		"unknown kind": {`{"t":"hello","v":2,"token":"x"}`, `{"t":"meta"}`},
		"old version":  {`{"t":"hello","v":1,"token":"x"}`},
	} {
		c := r.dial(webTestOpen)
		for _, l := range lines {
			c.send(l)
		}
		var errLine string
		for {
			line, ok := r.next(c)
			if !ok {
				break
			}
			if strings.Contains(line, `"t":"err"`) {
				errLine = line
			}
		}
		if !strings.Contains(errLine, `"code":"bad_request"`) {
			t.Errorf("%s: last err %q, want bad_request then close", name, errLine)
		}
	}
	ctl := r.dial(`{"t":"ctl"}`)
	ctl.send(`{"t":"hello","v":2,"token":"x"}`)
	r.waitEOF(ctl)

	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"bye"}`)
	r.waitEOF(c)
}
