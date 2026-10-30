//go:build unix

package app

import (
	"testing"
	"time"
)

// webResumeRig: a hub with the test topics and a fake clock for the
// resume window.
func webResumeRig(t *testing.T) (*webRig, *int, *time.Time) {
	r := newWebRig(t, nil)
	n, log := 0, ""
	webTestTopics(r, &n, &log)
	now := time.Unix(5000, 0)
	old := webNow
	t.Cleanup(func() { webNow = old })
	webNow = func() time.Time { return now }
	return r, &n, &now
}

// webDropConn closes a browser connection and waits until Update forgot it.
func (r *webRig) webDropConn(c *webClient) {
	r.t.Helper()
	_ = c.c.Close()
	r.pumpUntil("the connection gone", func() bool { return len(r.hub.live) == 0 })
}

// webExpectDelta reads one delta and checks its seq and n.
func (r *webRig) webExpectDelta(c *webClient, seq, n int) {
	r.t.Helper()
	d := r.nextMsg(c)
	data, _ := d["data"].(map[string]any)
	if d["t"] != "delta" || d["seq"] != float64(seq) || data["n"] != float64(n) {
		r.t.Fatalf("want delta seq %d n %d, got %v", seq, n, d)
	}
}

// Spec (WEB-7): a browser that comes back within 60 s with hello.resume
// (its old connection id and the last seq it saw per topic) gets the
// deltas it missed, with the seqs going on from there, and no snap:
// those it lost in flight and those published while it was away.
func TestResumeWithin60s(t *testing.T) {
	r, n, now := webResumeRig(t)
	c1, _ := r.webHello(webTestOpen)
	c1.send(`{"t":"sub","topics":["status"]}`)
	r.nextMsg(c1) // snap seq 1
	for i := 1; i <= 2; i++ {
		*n = i
		r.m.webPublish("status", map[string]int{"n": i})
	}
	r.webExpectDelta(c1, 2, 1)
	r.webExpectDelta(c1, 3, 2) // the browser did not get this one
	r.webDropConn(c1)
	*now = now.Add(30 * time.Second)
	*n = 3
	r.m.webPublish("status", map[string]int{"n": 3}) // while away

	c2 := r.dial(`{"t":"open","conn":"c-2","role":"controller","device":"loc-1"}`)
	c2.send(`{"t":"hello","v":2,"token":"x","resume":{"conn":"c-1","topics":{"status":2}}}`)
	if meta := r.nextMsg(c2); meta["t"] != "meta" || meta["conn"] != "c-2" {
		t.Fatalf("meta %v", meta)
	}
	r.webExpectDelta(c2, 3, 2)
	r.webExpectDelta(c2, 4, 3)
	*n = 4
	r.m.webPublish("status", map[string]int{"n": 4})
	r.webExpectDelta(c2, 5, 4)
	if len(r.hub.resume) != 0 {
		t.Fatalf("the resume entry was not used up: %v", r.hub.resume)
	}
}
