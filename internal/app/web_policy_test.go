package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webGoldenArgs is a valid args JSON for op, from the protocol goldens.
func webGoldenArgs(t *testing.T, op string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "webproto", "testdata", "golden", "ops", op+".json"))
	if err != nil {
		t.Fatalf("no golden for %s: %v", op, err)
	}
	var r webproto.Req
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r.Args
}

// webPolicyConn is a socketless hub connection with role; its requests
// go to h.ch as the reader goroutine would send them.
func webPolicyConn(h *webHub, id string, role webproto.Role) *webConn {
	c := &webConn{hub: h, id: id, role: role, device: "phone", out: newWebQueue(), subs: map[string]*webSub{}, hello: true}
	c.ctx, c.cancel = context.WithCancel(h.ctx)
	return c
}

// webPolicyReq sends one request on c and says how the hub took it:
// "forwarded" (on its way to Update; it is answered at once so the
// in-flight cap stays out of the way) or the err code.
func webPolicyReq(t *testing.T, c *webConn, op string, args json.RawMessage) string {
	t.Helper()
	if !c.request(&webproto.Req{T: webproto.KindReq, ID: c.lastID + 1, Op: op, Args: args}) {
		return "closed"
	}
	select {
	case msg := <-c.hub.ch:
		call := msg.(webCallMsg).call
		call.reply(nil)
		webTestTake(t, c) // the res
		return "forwarded"
	default:
	}
	fs := webTestTake(t, c)
	if len(fs) != 1 || fs[0]["t"] != "err" || fs[0]["id"] != float64(c.lastID) {
		t.Fatalf("%s: frames %v", op, fs)
	}
	return fs[0]["code"].(string)
}

// Spec (3.2, 3.5, T46): the hub enforces every row of the op table on
// its own, whatever the gateway did: a browser may never send a control
// op, a viewer never a controller op, the control connection never a
// browser op; args over the row's size are "limit"; the row's rate is
// kept with 2x headroom (then "limit" until the clock moves on).
func TestPolicyTableEnforcedInHub(t *testing.T) {
	h := newWebHub()
	t.Cleanup(h.stop)
	now := webTestClock(t)
	for i, p := range webproto.Ops() {
		args := webGoldenArgs(t, p.Op)
		id := "c-" + p.Op
		viewer := webPolicyConn(h, id+"-v", webproto.RoleViewer)
		ctrl := webPolicyConn(h, id+"-c", webproto.RoleController)
		ctl := webPolicyConn(h, id+"-ctl", webproto.RoleCtl)
		owner, others := ctrl, []*webConn{ctl}
		switch p.Role {
		case webproto.RoleViewer:
			owner = viewer
		case webproto.RoleCtl:
			owner, others = ctl, []*webConn{viewer, ctrl}
		default:
			others = append(others, viewer)
		}
		for _, o := range others {
			if got := webPolicyReq(t, o, p.Op, args); got != string(webproto.CodeForbidden) {
				t.Errorf("%s from %s: %s, want forbidden", p.Op, o.role, got)
			}
		}
		big := json.RawMessage(`{"x":"` + strings.Repeat("a", p.MaxBytes) + `"}`)
		if got := webPolicyReq(t, owner, p.Op, big); got != string(webproto.CodeLimit) {
			t.Errorf("%s with %d bytes: %s, want limit", p.Op, len(big), got)
		}
		// The rate: 2x the row's burst passes at one instant, then no more.
		allow := min(2*p.Rate.N, webHubReqBurst)
		for n := 0; n < allow; n++ {
			if got := webPolicyReq(t, owner, p.Op, args); got != "forwarded" {
				t.Fatalf("%s call %d of %d: %s", p.Op, n+1, allow, got)
			}
		}
		if got := webPolicyReq(t, owner, p.Op, args); got != string(webproto.CodeLimit) {
			t.Errorf("%s over 2x its rate: %s, want limit", p.Op, got)
		}
		*now = now.Add(p.Rate.Per)
		if got := webPolicyReq(t, owner, p.Op, args); got != "forwarded" {
			t.Errorf("%s after %s: %s", p.Op, p.Rate.Per, got)
		}
		*now = now.Add(time.Duration(i+1) * time.Hour) // a fresh start for the next op
	}
}

// Spec (3.2, 3.9): an op whose row says LaptopCard can only run through
// a laptop card: its handler is a card builder (webAskOps), never a plain
// op handler, and the hub routes it through the card even if a plain
// handler were registered by mistake. Every such browser op has one.
func TestLaptopCardOpsOnlyThroughTheCard(t *testing.T) {
	for _, p := range webproto.Ops() {
		if !p.LaptopCard || p.Role == webproto.RoleCtl {
			continue
		}
		if webAskOps[p.Op] == nil {
			t.Errorf("%s needs a laptop card but has no card builder", p.Op)
		}
		if webOps[p.Op] != nil {
			t.Errorf("%s needs a laptop card but has a plain handler", p.Op)
		}
	}

	m := aiTestModel(t)
	h := webTestHub(t, &m)
	ran := false
	h.ops = map[string]webOpFunc{webproto.OpTermClose: func(m *Model, call *webCall) tea.Cmd {
		ran = true
		call.reply(nil)
		return nil
	}}
	c := webTestSub(t, &m)
	call := &webCall{ctx: c.ctx, conn: c, id: 1, op: webproto.OpTermClose, args: &webproto.BufArgs{Buf: 7}}
	c.inflight.Add(1)
	m.onWebCall(webCallMsg{hub: h, call: call})
	if ran {
		t.Fatal("a plain handler ran an op that needs a laptop card")
	}
	if fs := webTestNow(t, c); len(fs) != 1 || fs[0]["t"] != "err" {
		t.Fatalf("answer %v", fs)
	}
}
