//go:build unix

package webgw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// goldenArgs is a valid args JSON for op, from the protocol goldens.
func goldenArgs(t *testing.T, op string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "webproto", "testdata", "golden", "ops", op+".json"))
	if err != nil {
		t.Fatalf("no golden for %s: %v", op, err)
	}
	var r struct {
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return string(r.Args)
}

// walkConn is one browser in the walk with its own request ids.
type walkConn struct {
	c  *websocket.Conn
	p  *hubPeer
	id int
}

func (w *walkConn) req(t *testing.T, op, args string) int {
	t.Helper()
	w.id++
	writeText(t, w.c, `{"t":"req","id":`+itoa(w.id)+`,"op":"`+op+`","args":`+args+`}`)
	return w.id
}

// forwarded checks the hub got request id and answers it.
func (w *walkConn) forwarded(t *testing.T, op string, id int) {
	t.Helper()
	m := w.p.line(t)
	if m["op"] != op || m["id"] != float64(id) {
		t.Fatalf("%s: hub got %v, want id %d", op, m, id)
	}
	w.p.send(t, `{"t":"res","id":`+itoa(id)+`}`)
	if a := readMsg(t, w.c); a["t"] != "res" {
		t.Fatalf("%s: browser got %v", op, a)
	}
}

// Spec (3.2, 3.5, T46): the gateway enforces every row of the op table
// before the hub sees anything: a viewer's controller op and any
// browser's control op are refused (forbidden), args over the row's size
// are refused (limit), and over the row's rate (or the 20/s, burst 40 of
// all requests) the gateway stops reading until there is room; a
// laptop-card row is a controller op with a slow rate and an audit line.
func TestPolicyTableEnforcedInGateway(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	vtok, _, err := r.tokens.Issue("loc-view", RoleViewer, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	vc, vp := r.connectWith(vtok)
	cc, cp := r.connect()
	viewer, ctrl := &walkConn{c: vc, p: vp}, &walkConn{c: cc, p: cp}
	for _, p := range webproto.Ops() {
		args := goldenArgs(t, p.Op)
		if p.LaptopCard && (p.Role == webproto.RoleViewer || p.Audit != webproto.AuditYes || p.Rate.Per/time.Duration(p.Rate.N) < time.Second) {
			t.Errorf("%s: a laptop-card row must be a controller op, audited, at most 1/s: %+v", p.Op, p)
		}
		// Role.
		switch p.Role {
		case webproto.RoleViewer:
			viewer.forwarded(t, p.Op, viewer.req(t, p.Op, args))
		case webproto.RoleController:
			wantErr(t, viewer.c, float64(viewer.req(t, p.Op, args)), webproto.CodeForbidden)
		case webproto.RoleCtl:
			wantErr(t, viewer.c, float64(viewer.req(t, p.Op, args)), webproto.CodeForbidden)
			wantErr(t, ctrl.c, float64(ctrl.req(t, p.Op, args)), webproto.CodeForbidden)
			continue
		}
		// Size.
		big := `{"x":"` + strings.Repeat("a", p.MaxBytes) + `"}`
		wantErr(t, ctrl.c, float64(ctrl.req(t, p.Op, big)), webproto.CodeLimit)
		// Rate: a full burst passes at one instant, the next waits.
		r.clock.Add(max(p.Rate.Per, 2*time.Second)) // full buckets
		burst := min(p.Rate.N, reqBurst)
		for n := 0; n < burst; n++ {
			ctrl.forwarded(t, p.Op, ctrl.req(t, p.Op, args))
		}
		held := ctrl.req(t, p.Op, args)
		noHubLine(t, ctrl.p)
		r.clock.Add(max(p.Rate.Per/time.Duration(p.Rate.N), time.Second/reqPerSecond))
		ctrl.forwarded(t, p.Op, held)
	}
	noHubLine(t, viewer.p)
}
