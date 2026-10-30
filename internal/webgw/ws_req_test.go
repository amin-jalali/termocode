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

func discard(id int) string {
	return `{"t":"req","id":` + itoa(id) + `,"op":"git.discard","args":{"paths":["a.go"]}}`
}

// Spec (3.5): over an op's rate the gateway stops reading that browser
// (backpressure) instead of refusing; the message goes on once there is
// room again.
func TestRateLimitBackpressure(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c, p := r.connect()
	writeText(t, c, discard(1))
	writeText(t, c, discard(2)) // git.discard: 1 per 5 s
	if m := p.line(t); m["id"] != float64(1) {
		t.Fatalf("hub got %v", m)
	}
	p.send(t, `{"t":"res","id":1}`)
	readMsg(t, c)
	noHubLine(t, p)
	r.clock.Add(5 * time.Second)
	if m := p.line(t); m["id"] != float64(2) {
		t.Fatalf("hub got %v", m)
	}
}

// Spec (3.5): held back by the limits for ThrottleMax in one go, the
// connection is closed with 1008.
func TestRateLimitTooLongCloses1008(t *testing.T) {
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) { c.ThrottleMax = 200 * time.Millisecond })
	c, p := r.connect()
	writeText(t, c, discard(1))
	writeText(t, c, discard(2))
	p.line(t)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.ClosePolicy) {
		t.Fatalf("closed with %d, want 1008", got)
	}
	if !strings.Contains(auditText(t, r.alog), `"ev":"rate_limited"`) {
		t.Fatal("no rate_limited in the audit log")
	}
}

// Spec (1.4): at most 4 requests wait for the hub; the 5th goes once one
// is answered.
func TestInFlightCap(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c, p := r.connect()
	for i := 1; i <= MaxInFlight+1; i++ {
		writeText(t, c, `{"t":"req","id":`+itoa(i)+`,"op":"tree.list","args":{"path":""}}`)
	}
	for i := 1; i <= MaxInFlight; i++ {
		if m := p.line(t); m["id"] != float64(i) {
			t.Fatalf("hub got %v", m)
		}
	}
	noHubLine(t, p)
	p.send(t, `{"t":"err","id":2,"code":"busy","msg":"x"}`)
	if m := p.line(t); m["id"] != float64(MaxInFlight+1) {
		t.Fatalf("hub got %v", m)
	}
}

// Spec (3.2): term.send is capped at 1 MiB per second.
func TestTermSendByteRate(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c, p := r.connect()
	text := strings.Repeat("y", 60<<10)
	sent := 0
	for i := 1; sent+len(text) <= 1<<20; i++ {
		writeText(t, c, `{"t":"req","id":`+itoa(i)+`,"op":"term.send","args":{"buf":1,"text":"`+text+`"}}`)
		m := p.line(t)
		p.send(t, `{"t":"res","id":`+itoa(int(m["id"].(float64)))+`}`)
		readMsg(t, c)
		sent += len(text) + len(`{"buf":1,"text":""}`)
	}
	writeText(t, c, `{"t":"req","id":99,"op":"term.send","args":{"buf":1,"text":"`+text+`"}}`)
	noHubLine(t, p)
	r.clock.Add(time.Second)
	if m := p.line(t); m["id"] != float64(99) {
		t.Fatalf("hub got %v", m)
	}
}
