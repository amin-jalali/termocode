package app

import (
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-32): a wake goes out only when more cards wait than at the
// last check; a card answered and a new one coming is a new wake, the
// same count again is not.
func TestWebPushDue(t *testing.T) {
	var s webPushWakeState
	steps := []struct {
		n    int
		want bool
	}{{0, false}, {1, true}, {1, false}, {2, true}, {0, false}, {1, true}, {3, true}, {3, false}}
	for i, st := range steps {
		if got := s.due(st.n); got != st.want {
			t.Errorf("step %d (n=%d): due %v, want %v", i, st.n, got, st.want)
		}
	}
}

// Spec (MOB-32): web.push_watch from the gateway turns wakes on and off
// for that control connection only; with it on, a new waiting card sends
// one wake event with the count and nothing else on the control
// connection; a browser connection never gets one.
func TestWebPushWatchAndWake(t *testing.T) {
	h := newWebHub()
	t.Cleanup(h.stop)
	ctl := webPolicyConn(h, "ctl-1", webproto.RoleCtl)
	m := &Model{web: h}

	call := &webCall{ctx: ctl.ctx, conn: ctl, id: 1, op: webproto.OpWebPushWatch, args: &webproto.PushWatchArgs{On: true}}
	m.webPushWatch(call)
	if !h.wake.on[ctl] {
		t.Fatal("watch not on")
	}
	webTestTake(t, ctl) // the res

	data := h.wake.wakeFor(2)
	if len(data) == 0 {
		t.Fatal("no wake for 2 cards")
	}
	var note map[string]any
	if err := json.Unmarshal(data, &note); err != nil || len(note) != 1 || note["count"] != float64(2) {
		t.Fatalf("wake %s %v", data, err)
	}
	if h.wake.wakeFor(2) != nil {
		t.Fatal("a second wake for the same count")
	}

	off := &webCall{ctx: ctl.ctx, conn: ctl, id: 2, op: webproto.OpWebPushWatch, args: &webproto.PushWatchArgs{On: false}}
	m.webPushWatch(off)
	if len(h.wake.on) != 0 || h.wake.last != 0 {
		t.Fatalf("watch still on: %v last %d", h.wake.on, h.wake.last)
	}

	viewer := webPolicyConn(h, "c-1", webproto.RoleViewer)
	bad := &webCall{ctx: viewer.ctx, conn: viewer, id: 3, op: webproto.OpWebPushWatch, args: &webproto.PushWatchArgs{On: true}}
	m.webPushWatch(bad)
	if h.wake.on[viewer] {
		t.Fatal("a browser turned wakes on")
	}
}
