//go:build unix

package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestTopics registers "status" (state: {"n": *n, "log": text}) and
// "buf" (keyed) for one rig.
func webTestTopics(r *webRig, n *int, log *string) {
	r.hub.topics = map[string]webTopicDef{
		"status": {
			snapshot: func(m *Model, key string) (any, error) {
				return map[string]any{"n": *n, "log": *log}, nil
			},
			apply: webTestApply,
		},
		"buf": {
			snapshot: func(m *Model, key string) (any, error) {
				if key == "999" {
					return nil, webErr(webproto.CodeNotFound, "No such buffer.")
				}
				return map[string]string{"buf": key}, nil
			},
		},
	}
}

// webTestApply is the status topic's apply: a delta {"n": x} sets n, a
// text append {"id":"log","append":s} appends to log.
func webTestApply(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s struct {
		N   int    `json:"n"`
		Log string `json:"log"`
	}
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	var d struct {
		N      *int   `json:"n"`
		ID     string `json:"id"`
		Append string `json:"append"`
	}
	if err := json.Unmarshal(delta, &d); err != nil {
		return nil, err
	}
	if d.N != nil {
		s.N = *d.N
	}
	if d.ID == "log" {
		s.Log += d.Append
	}
	return json.Marshal(s)
}

// Spec (WEB-7): sub answers each topic with a snap built on Update (seq
// counts per connection and topic from 1); an unknown view, a failing
// snapshot and the per-connection caps (16 buf topics) are one err each
// with the topic; unsub stops the topic.
func TestSubSendsSnap(t *testing.T) {
	r := newWebRig(t, nil)
	n, log := 3, ""
	webTestTopics(r, &n, &log)
	c, _ := r.webHello(`{"t":"open","conn":"v-1","role":"viewer","device":"d"}`)
	c.send(`{"t":"sub","topics":["status"]}`)
	snap := r.nextMsg(c)
	if snap["t"] != "snap" || snap["topic"] != "status" || snap["seq"] != float64(1) {
		t.Fatalf("snap %v", snap)
	}
	if d, _ := snap["data"].(map[string]any); d["n"] != float64(3) {
		t.Fatalf("data %v", snap["data"])
	}
	c.send(`{"t":"sub","topics":["git","buf:999"]}`)
	for _, want := range []string{"git", "buf:999"} {
		e := r.nextMsg(c)
		d, _ := e["data"].(map[string]any)
		if e["t"] != "err" || e["code"] != "not_found" || d["topic"] != want {
			t.Fatalf("err for %s: %v", want, e)
		}
	}
	var topics []string
	for i := 1; i <= webMaxBufTopics+1; i++ {
		topics = append(topics, `"buf:`+strconv.Itoa(i)+`"`)
	}
	c.send(fmt.Sprintf(`{"t":"sub","topics":[%s]}`, join(topics)))
	for i := 1; i <= webMaxBufTopics; i++ {
		if s := r.nextMsg(c); s["t"] != "snap" {
			t.Fatalf("buf %d: %v", i, s)
		}
	}
	if e := r.nextMsg(c); e["code"] != "limit" {
		t.Fatalf("over the buf cap: %v", e)
	}
	c.send(`{"t":"unsub","topics":["status"]}`)
	n = 4
	c.send(`{"t":"sub","topics":["status"]}`)
	snap = r.nextMsg(c)
	if snap["seq"] != float64(1) { // unsub forgot the topic: a new sub starts over
		t.Fatalf("second snap %v", snap)
	}
	if d, _ := snap["data"].(map[string]any); d["n"] != float64(4) {
		t.Fatalf("data %v", snap["data"])
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

// Spec (WEB-7): webPublish (on Update) sends a delta to every connection
// subscribed to the topic, with the next seq of that connection; others
// get nothing; a gone connection is forgotten. Without a hub it is a
// no-op.
func TestPublishSeq(t *testing.T) {
	var none Model
	none.webPublish("status", map[string]int{"n": 1}) // no hub: nothing

	r := newWebRig(t, nil)
	n, log := 1, ""
	webTestTopics(r, &n, &log)
	a, _ := r.webHello(webTestOpen)
	b, _ := r.webHello(`{"t":"open","conn":"c-2","role":"controller","device":"d"}`)
	a.send(`{"t":"sub","topics":["status"]}`)
	r.nextMsg(a)
	r.m.webPublish("status", map[string]int{"n": 2})
	b.send(`{"t":"sub","topics":["status","buf:1"]}`)
	r.nextMsg(b)
	r.nextMsg(b)
	r.m.webPublish("status", map[string]int{"n": 3})
	r.m.webPublish("tree", map[string]int{"x": 1}) // nobody listens

	for _, tc := range []struct {
		c    *webClient
		seqs []float64
		ns   []float64
	}{{a, []float64{2, 3}, []float64{2, 3}}, {b, []float64{2}, []float64{3}}} {
		for i := range tc.seqs {
			d := r.nextMsg(tc.c)
			data, _ := d["data"].(map[string]any)
			if d["t"] != "delta" || d["topic"] != "status" || d["seq"] != tc.seqs[i] || data["n"] != tc.ns[i] {
				t.Fatalf("delta %d: %v", i, d)
			}
		}
	}
	_ = b.c.Close()
	r.pumpUntil("b gone", func() bool { return len(r.hub.live) == 1 })
	r.m.webPublish("status", map[string]int{"n": 4})
	if d := r.nextMsg(a); d["seq"] != float64(4) {
		t.Fatalf("after b left: %v", d)
	}
}

// Spec (WEB-7, T41): publish never waits for a browser. A gateway that
// stops reading fills its queue; every publish still returns at once, the
// topic turns stale, and when the gateway reads again it gets the queued
// deltas in order, one resync, and a fresh snap after it subscribes again.
func TestPublishNeverBlocks(t *testing.T) {
	r := newWebRig(t, nil)
	n, log := 0, ""
	webTestTopics(r, &n, &log)
	nc, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	if _, err := nc.Write([]byte(webTestOpen + "\n" + `{"t":"hello","v":2,"token":"x"}` + "\n" + `{"t":"sub","topics":["status"]}` + "\n")); err != nil {
		t.Fatal(err)
	}
	var hc *webConn
	r.pumpUntil("the subscription", func() bool {
		r.hub.mu.Lock()
		hc = r.hub.byID["c-1"]
		r.hub.mu.Unlock()
		return hc != nil && hc.subs["status"] != nil
	})
	pad := strings.Repeat("p", 1024)
	start := time.Now()
	var worst time.Duration
	for i := 1; i <= 5000; i++ {
		n = i
		t0 := time.Now()
		r.m.webPublish("status", map[string]any{"n": i, "pad": pad})
		if d := time.Since(t0); d > worst {
			worst = d
		}
	}
	if !hc.out.isStale("status") {
		t.Fatal("the topic never turned stale")
	}
	if total := time.Since(start); total > 20*time.Second || worst > time.Second { // loose for slow CI
		t.Fatalf("publish waited: total %s, worst %s", total, worst)
	}
	// The gateway reads again.
	br := bufio.NewReader(nc)
	last := uint64(0)
	for {
		_ = nc.SetReadDeadline(time.Now().Add(10 * time.Second))
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v (last seq %d)", err, last)
		}
		var m struct {
			T   string `json:"t"`
			Seq uint64 `json:"seq"`
		}
		_ = json.Unmarshal([]byte(line), &m)
		if m.T == "resync" {
			break
		}
		if m.T == "meta" {
			continue
		}
		if m.Seq != last+1 {
			t.Fatalf("seq %d after %d", m.Seq, last)
		}
		last = m.Seq
	}
	if _, err := nc.Write([]byte(`{"t":"sub","topics":["status"]}` + "\n")); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		line, _ := br.ReadString('\n')
		got <- line
	}()
	var line string
	r.pumpUntil("the new snap", func() bool {
		select {
		case line = <-got:
			return true
		default:
			return false
		}
	})
	if !strings.Contains(line, `"t":"snap"`) || !strings.Contains(line, `"n":5000`) || !strings.Contains(line, fmt.Sprintf(`"seq":%d`, last+1)) {
		t.Fatalf("after resync: %.200s", line)
	}
}

// Spec (WEB-7): a gateway that stops reading for longer than the write
// timeout is closed (it then closes the browser with 4008); the editor
// and the other connections go on.
func TestSlowGatewayIsClosed(t *testing.T) {
	old := webWriteTimeout.Swap(int64(100 * time.Millisecond))
	t.Cleanup(func() { webWriteTimeout.Store(old) })
	r := newWebRig(t, nil)
	n, log := 0, ""
	webTestTopics(r, &n, &log)
	nc, err := net.Dial("unix", r.path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	if _, err := nc.Write([]byte(webTestOpen + "\n" + `{"t":"hello","v":2,"token":"x"}` + "\n" + `{"t":"sub","topics":["status"]}` + "\n")); err != nil {
		t.Fatal(err)
	}
	other, _ := r.webHello(`{"t":"open","conn":"c-2","role":"controller","device":"d"}`)
	r.pumpUntil("the subscription", func() bool { return len(r.hub.live) == 2 && webTestSubs(r.hub) == 1 })
	pad := strings.Repeat("p", 64<<10)
	for i := 0; r.hub.conns() == 2; i++ {
		r.m.webPublish("status", map[string]any{"n": i, "pad": pad})
		r.pumpUntil("a moment", func() bool { return true })
		if i > 100000 {
			t.Fatal("the stalled connection was never closed")
		}
	}
	other.send(`{"t":"ping","n":"still"}`)
	if p := r.nextMsg(other); p["t"] != "pong" {
		t.Fatalf("other connection: %v", p)
	}
}

// webTestSubs counts live connections with a subscription.
func webTestSubs(h *webHub) int {
	n := 0
	for c := range h.live {
		if len(c.subs) > 0 {
			n++
		}
	}
	return n
}
