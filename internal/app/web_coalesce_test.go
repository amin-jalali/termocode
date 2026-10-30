package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// webQueueRig is a hub with one subscribed connection whose queue nobody
// drains: what publish queues stays there for the test to take.
func webQueueRig(topics ...string) (*webHub, *webConn) {
	h := newWebHub()
	c := &webConn{hub: h, id: "c-q", out: newWebQueue(), subs: map[string]*webSub{}}
	c.ctx, c.cancel = context.WithCancel(h.ctx)
	for _, t := range topics {
		c.subs[t] = &webSub{seq: 1}
	}
	h.live[c] = true
	return h, c
}

// webTestTake decodes every queued frame.
func webTestTake(t *testing.T, c *webConn) []map[string]any {
	t.Helper()
	frames, _ := c.out.take()
	var out []map[string]any
	for _, f := range frames {
		var v map[string]any
		if err := json.Unmarshal(f, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// Spec (WEB-7): text appends to the same id merge into the newest queued
// delta of the topic (one frame, one seq); another id, another topic or a
// delta the writer already took starts a new frame.
func TestCoalesceAppend(t *testing.T) {
	h, c := webQueueRig("chat", "toasts")
	h.publish("chat", webTextAppend{ID: "m1", Append: "Hel"})
	h.publish("chat", webTextAppend{ID: "m1", Append: "lo"})
	h.publish("chat", webTextAppend{ID: "m1", Append: ", world"})
	h.publish("toasts", webTextAppend{ID: "m1", Append: "other topic"})
	got := webTestTake(t, c)
	if len(got) != 2 {
		t.Fatalf("%d frames: %v", len(got), got)
	}
	if d, _ := got[0]["data"].(map[string]any); d["append"] != "Hello, world" || got[0]["seq"] != float64(2) {
		t.Fatalf("merged: %v", got[0])
	}
	if c.subs["chat"].seq != 2 {
		t.Fatalf("seq %d: a merge does not use a seq", c.subs["chat"].seq)
	}
	h.publish("chat", webTextAppend{ID: "m1", Append: "!"}) // the first frame is gone
	h.publish("chat", webTextAppend{ID: "m2", Append: "new"})
	got = webTestTake(t, c)
	if len(got) != 2 || got[0]["seq"] != float64(3) || got[1]["seq"] != float64(4) {
		t.Fatalf("after take: %v", got)
	}
	// A snap queued last is never merged into.
	c.out.pushTopic("chat", 5, nil, []byte(`{"t":"snap"}`))
	c.subs["chat"].seq = 5
	h.publish("chat", webTextAppend{ID: "m2", Append: "x"})
	if got := webTestTake(t, c); len(got) != 2 || !strings.Contains(got[1]["t"].(string), "delta") {
		t.Fatalf("after a snap: %v", got)
	}
}

// webTestApplyLines applies a lines delta the way the page does.
func webTestApplyLines(lines []string, d webLinesDelta) []string {
	out := append([]string(nil), lines[:d.First]...)
	out = append(out, d.Lines...)
	return append(out, lines[d.Last:]...)
}

// Spec (WEB-7): a lines delta (replace [first, last) with lines) merges
// with the queued one when it replaces exactly the lines that one wrote
// (a row redrawn again); the merged delta does what both did. Other
// ranges, and other shapes, start a new frame.
func TestCoalesceLines(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e"}
	for _, tc := range []struct {
		name      string
		d1, d2    webLinesDelta
		wantMerge bool
	}{
		{"same row again", webLinesDelta{First: 2, Last: 3, Lines: []string{"C"}}, webLinesDelta{First: 2, Last: 3, Lines: []string{"CC"}}, true},
		{"grown block redrawn", webLinesDelta{First: 1, Last: 2, Lines: []string{"x", "y"}}, webLinesDelta{First: 1, Last: 3, Lines: []string{"z"}}, true},
		{"insert then redraw it", webLinesDelta{First: 5, Last: 5, Lines: []string{"f"}}, webLinesDelta{First: 5, Last: 6, Lines: []string{"F", "G"}}, true},
		{"other row", webLinesDelta{First: 2, Last: 3, Lines: []string{"C"}}, webLinesDelta{First: 3, Last: 4, Lines: []string{"D"}}, false},
		{"wider range", webLinesDelta{First: 2, Last: 3, Lines: []string{"C"}}, webLinesDelta{First: 1, Last: 4, Lines: []string{"Q"}}, false},
	} {
		h, c := webQueueRig("term:7")
		h.publish("term:7", tc.d1)
		h.publish("term:7", tc.d2)
		got := webTestTake(t, c)
		if merged := len(got) == 1; merged != tc.wantMerge {
			t.Errorf("%s: %d frames", tc.name, len(got))
			continue
		}
		want := webTestApplyLines(webTestApplyLines(base, tc.d1), tc.d2)
		var have []string
		if tc.wantMerge {
			var d webLinesDelta
			b, _ := json.Marshal(got[0]["data"])
			_ = json.Unmarshal(b, &d)
			have = webTestApplyLines(base, d)
		} else {
			have = want
		}
		if strings.Join(have, ",") != strings.Join(want, ",") {
			t.Errorf("%s: merged gives %v, both give %v", tc.name, have, want)
		}
	}
	h, c := webQueueRig("term:7")
	h.publish("term:7", webLinesDelta{First: 0, Last: 1, Lines: []string{"a"}})
	h.publish("term:7", webTextAppend{ID: "x", Append: "y"})
	if got := webTestTake(t, c); len(got) != 2 {
		t.Fatalf("shapes mixed: %v", got)
	}
}
