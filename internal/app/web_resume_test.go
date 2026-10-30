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

// Spec (WEB-7): after 60 s, for an unknown connection, for a topic that
// was stale, or when more deltas were missed than the ring keeps, resume
// falls back to a fresh snap of each topic the browser listed.
func TestResumeAfter60sSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name  string
		away  time.Duration
		conn  string
		extra int // deltas published while away
	}{
		{"after 60 s", webResumeFor + time.Second, "c-1", 1},
		{"unknown connection", time.Second, "c-9", 1},
		{"ring overflow", time.Second, "c-1", webRingLen + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, n, now := webResumeRig(t)
			c1, _ := r.webHello(webTestOpen)
			c1.send(`{"t":"sub","topics":["status"]}`)
			r.nextMsg(c1)
			r.webDropConn(c1)
			*now = now.Add(tc.away)
			for i := 1; i <= tc.extra; i++ {
				*n = i
				r.m.webPublish("status", map[string]int{"n": i})
			}
			c2 := r.dial(`{"t":"open","conn":"c-2","role":"controller","device":"loc-1"}`)
			c2.send(`{"t":"hello","v":2,"token":"x","resume":{"conn":"` + tc.conn + `","topics":{"status":1}}}`)
			r.nextMsg(c2) // meta
			snap := r.nextMsg(c2)
			data, _ := snap["data"].(map[string]any)
			if snap["t"] != "snap" || snap["seq"] != float64(1) || data["n"] != float64(tc.extra) {
				t.Fatalf("want a fresh snap, got %v", snap)
			}
			if want := map[bool]int{true: 1, false: 0}[tc.conn != "c-1"]; len(r.hub.resume) != want {
				t.Fatalf("resume table %v, want %d entries", r.hub.resume, want) // another id's entry stays
			}
			if tc.name == "after 60 s" && len(r.hub.rings) != 1 {
				t.Fatalf("rings %d: only the live topic keeps one", len(r.hub.rings))
			}
		})
	}
	// A topic that was stale when the connection dropped.
	r, _, _ := webResumeRig(t)
	c1, _ := r.webHello(webTestOpen)
	c1.send(`{"t":"sub","topics":["status"]}`)
	r.nextMsg(c1)
	r.hub.mu.Lock()
	hc := r.hub.byID["c-1"]
	r.hub.mu.Unlock()
	hc.out.mu.Lock()
	hc.out.stale["status"] = true
	hc.out.mu.Unlock()
	r.webDropConn(c1)
	c2 := r.dial(`{"t":"open","conn":"c-2","role":"controller","device":"loc-1"}`)
	c2.send(`{"t":"hello","v":2,"token":"x","resume":{"conn":"c-1","topics":{"status":1}}}`)
	r.nextMsg(c2)
	if snap := r.nextMsg(c2); snap["t"] != "snap" {
		t.Fatalf("stale topic: %v", snap)
	}
}
