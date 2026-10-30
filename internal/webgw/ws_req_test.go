//go:build unix

package webgw

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// noHubLine checks the hub got nothing more on p for a moment.
func noHubLine(t *testing.T, p *hubPeer) {
	t.Helper()
	select {
	case l, ok := <-p.lines:
		if ok {
			t.Fatalf("the hub got %s", l)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func wantErr(t *testing.T, c *websocket.Conn, id float64, code webproto.ErrCode) {
	t.Helper()
	m := readMsg(t, c)
	if m["t"] != "err" || m["id"] != id || m["code"] != string(code) {
		t.Fatalf("got %v, want err %s for id %v", m, code, id)
	}
}

// Spec (T46, gateway half): a viewer's mutating op, a control-only op
// and an unknown op are answered by the gateway and never reach the hub;
// a read op from a viewer and a mutating op from a controller pass.
func TestViewerOpsRefusedInGatewayAndHub(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	vtok, _, err := r.tokens.Issue("loc-view", RoleViewer, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, p := r.connectWith(vtok)
	for i, op := range []string{webproto.OpTermSend, webproto.OpCardsAnswer, webproto.OpGitDiscard, webproto.OpWebStop} {
		writeText(t, c, `{"t":"req","id":`+itoa(i+1)+`,"op":"`+op+`"}`)
		wantErr(t, c, float64(i+1), webproto.CodeForbidden)
	}
	noHubLine(t, p)
	writeText(t, c, `{"t":"req","id":10,"op":"tree.list","args":{"path":""}}`)
	if m := p.line(t); m["op"] != "tree.list" {
		t.Fatalf("hub got %v", m)
	}
	if !strings.Contains(auditText(t, r.alog), `"why":"forbidden"`) {
		t.Fatal("refused op not in the audit log")
	}

	c2, p2 := r.connect()
	writeText(t, c2, `{"t":"req","id":1,"op":"web.open"}`)
	wantErr(t, c2, 1, webproto.CodeForbidden)
	writeText(t, c2, `{"t":"req","id":2,"op":"no.such"}`)
	wantErr(t, c2, 2, webproto.CodeBadRequest)
	big := strings.Repeat("x", 2<<10)
	writeText(t, c2, `{"t":"req","id":3,"op":"chat.open","args":{"chat":"`+big+`"}}`)
	wantErr(t, c2, 3, webproto.CodeLimit)
	writeText(t, c2, `{"t":"req","id":4,"op":"chat.open","args":{"nope":1}}`)
	wantErr(t, c2, 4, webproto.CodeBadRequest)
	noHubLine(t, p2)
	writeText(t, c2, `{"t":"req","id":5,"op":"term.interrupt","args":{"buf":3}}`)
	if m := p2.line(t); m["op"] != "term.interrupt" {
		t.Fatalf("hub got %v", m)
	}
}

// Spec (1.4): request ids go up strictly; a reused one is 1008.
func TestRequestIDsMustGoUp(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c, p := r.connect()
	writeText(t, c, `{"t":"req","id":5,"op":"tree.list","args":{"path":""}}`)
	p.line(t)
	writeText(t, c, `{"t":"req","id":5,"op":"tree.list","args":{"path":""}}`)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.ClosePolicy) {
		t.Fatalf("closed with %d, want 1008", got)
	}
}

func itoa(i int) string {
	const d = "0123456789"
	if i < 10 {
		return d[i : i+1]
	}
	return itoa(i/10) + d[i%10:i%10+1]
}
