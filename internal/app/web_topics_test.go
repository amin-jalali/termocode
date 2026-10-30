//go:build unix

package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

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
