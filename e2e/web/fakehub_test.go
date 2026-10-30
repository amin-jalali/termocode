//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webgw"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// wait is how long a test waits for the page to do something. Generous:
// macOS runners are slow; a passing run never waits this long.
const wait = 15 * time.Second

// FakeHub stands where the gateway's WebSocket and the hub will be. Each
// browser connection becomes a HubConn the test drives by hand. Every
// message from the page goes through webproto's strict decoder, so a page
// that speaks a field or a kind the Go side does not know fails the test.
type FakeHub struct {
	t      *testing.T
	tokens *webgw.Tokens
	conns  chan *HubConn

	mu       sync.Mutex
	refuse   bool
	attempts []time.Time // every GET /ws, refused or not
	live     []*HubConn
}

func newFakeHub(t *testing.T, tokens *webgw.Tokens) *FakeHub {
	return &FakeHub{t: t, tokens: tokens, conns: make(chan *HubConn, 16)}
}

// Refuse makes every new connection fail before the upgrade (HTTP 503),
// as a gateway without a hub would; the browser sees close code 1006.
func (h *FakeHub) Refuse(v bool) {
	h.mu.Lock()
	h.refuse = v
	h.mu.Unlock()
}

// Attempts returns when each GET /ws arrived.
func (h *FakeHub) Attempts() []time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.attempts)
}

func (h *FakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.attempts = append(h.attempts, time.Now())
	refuse := h.refuse
	h.mu.Unlock()
	if refuse {
		http.Error(w, "no hub", http.StatusServiceUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"termocode.v2"}})
	if err != nil {
		return
	}
	if ws.Subprotocol() != "termocode.v2" {
		h.t.Errorf("page asked for subprotocol %q, want termocode.v2", r.Header.Get("Sec-WebSocket-Protocol"))
		_ = ws.Close(websocket.StatusProtocolError, "subprotocol")
		return
	}
	ws.SetReadLimit(webproto.MaxFrame)
	ctx, cancel := context.WithCancel(context.Background())
	c := &HubConn{hub: h, ws: ws, ctx: ctx, cancel: cancel, in: make(chan webproto.Msg, 256), done: make(chan struct{})}
	h.mu.Lock()
	h.live = append(h.live, c)
	h.mu.Unlock()
	go c.readLoop()
	h.conns <- c
	<-c.done
}

// Accept waits for the page's next WebSocket connection.
func (h *FakeHub) Accept(t *testing.T) *HubConn {
	t.Helper()
	select {
	case c := <-h.conns:
		return c
	case <-time.After(wait):
		t.Fatalf("the page did not connect to /ws within %s", wait)
		return nil
	}
}

// NoConn checks that the page does not connect within d.
func (h *FakeHub) NoConn(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-h.conns:
		t.Fatalf("the page connected again; it should not have")
	case <-time.After(d):
	}
}

func (h *FakeHub) closeAll() {
	h.mu.Lock()
	live := slices.Clone(h.live)
	h.mu.Unlock()
	for _, c := range live {
		c.Drop()
	}
}

// HubConn is one browser connection.
type HubConn struct {
	hub    *FakeHub
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	in     chan webproto.Msg
	done   chan struct{}
	once   sync.Once

	mu    sync.Mutex
	pings int
	sent  []webproto.Msg // everything the page sent, pings included
}

func (c *HubConn) readLoop() {
	defer c.finish()
	for {
		typ, b, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			c.hub.t.Errorf("page sent a binary frame")
			return
		}
		m, err := webproto.DecodeClient(b)
		if err != nil {
			c.hub.t.Errorf("page sent a message the strict decoder refuses: %v: %s", err, b)
			return
		}
		c.mu.Lock()
		c.sent = append(c.sent, m)
		c.mu.Unlock()
		if p, ok := m.(*webproto.Ping); ok {
			c.mu.Lock()
			c.pings++
			c.mu.Unlock()
			c.send(&webproto.Pong{T: webproto.KindPong, N: p.N})
			continue
		}
		select {
		case c.in <- m:
		default:
			c.hub.t.Errorf("fake hub: inbox full")
			return
		}
	}
}

func (c *HubConn) finish() {
	c.once.Do(func() {
		c.cancel()
		close(c.done)
	})
}

// Done is closed when the connection is gone.
func (c *HubConn) Done() <-chan struct{} { return c.done }

// Sent returns every message the page sent on this connection so far.
func (c *HubConn) Sent() []webproto.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sent)
}

// Pings is the number of pings answered so far.
func (c *HubConn) Pings() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pings
}

// Next returns the page's next message. Pings never show up here: the
// fake hub answers them itself.
func (c *HubConn) Next(t *testing.T) webproto.Msg {
	t.Helper()
	select {
	case m := <-c.in:
		return m
	case <-c.done:
		t.Fatalf("the connection closed while waiting for a message")
	case <-time.After(wait):
		t.Fatalf("no message from the page within %s", wait)
	}
	return nil
}

// Quiet checks that the page sends nothing (but pings) for d.
func (c *HubConn) Quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case m := <-c.in:
		t.Fatalf("page sent %s; want nothing", show(m))
	case <-time.After(d):
	}
}

// Expect returns the next message, which must be a T.
func Expect[T webproto.Msg](t *testing.T, c *HubConn) T {
	t.Helper()
	m := c.Next(t)
	v, ok := m.(T)
	if !ok {
		var zero T
		t.Fatalf("page sent %s; want a %T", show(m), zero)
	}
	return v
}

func show(m webproto.Msg) string {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%T", m)
	}
	return string(b)
}

// Send writes one hub message to the page.
func (c *HubConn) Send(t *testing.T, m webproto.Msg) {
	t.Helper()
	if err := c.send(m); err != nil {
		t.Fatalf("send %s: %v", show(m), err)
	}
}

func (c *HubConn) send(m webproto.Msg) error {
	b, err := webproto.Encode(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// Close ends the connection with a close code, after a bye like the
// gateway sends.
func (c *HubConn) Close(t *testing.T, code webproto.CloseCode) {
	t.Helper()
	_ = c.send(&webproto.ServerBye{T: webproto.KindBye, Code: code})
	_ = c.ws.Close(websocket.StatusCode(code), code.Reason())
	c.finish()
}

// Drop cuts the connection without a close frame (the link died); the
// browser sees code 1006.
func (c *HubConn) Drop() {
	_ = c.ws.CloseNow()
	c.finish()
}

// Snap, Delta and Resync build topic messages; data is marshalled.
func Snap(topic string, seq uint64, data any) *webproto.Snap {
	return &webproto.Snap{T: webproto.KindSnap, Topic: topic, Seq: seq, Data: raw(data)}
}

func Delta(topic string, seq uint64, data any) *webproto.Delta {
	return &webproto.Delta{T: webproto.KindDelta, Topic: topic, Seq: seq, Data: raw(data)}
}

func Resync(topic string) *webproto.Resync {
	return &webproto.Resync{T: webproto.KindResync, Topic: topic}
}

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Meta is the meta a controller gets; conn names the connection.
func Meta(conn string) *webproto.Meta {
	return &webproto.Meta{T: webproto.KindMeta, V: 2, Conn: conn, Role: webproto.RoleController,
		Device: "loc-test", IdleS: 900, MaxS: 43200, Viewers: 1,
		UI: webproto.MetaUI{Theme: "termocode"}, Caps: []string{}}
}

// Handshake reads the page's hello, checks its token against the
// gateway's tokens, answers with meta for conn and returns the hello.
func (c *HubConn) Handshake(t *testing.T, conn string) *webproto.Hello {
	t.Helper()
	hello := Expect[*webproto.Hello](t, c)
	if hello.V != 2 {
		t.Fatalf("hello.v = %d, want 2", hello.V)
	}
	if _, err := c.hub.tokens.Check(hello.Token, time.Now()); err != nil {
		t.Fatalf("hello carries a token the gateway did not issue: %v", err)
	}
	c.Send(t, Meta(conn))
	return hello
}

// Subs reads sub messages until every topic in want was asked for; other
// subs are fine. It returns all topics asked for.
func (c *HubConn) Subs(t *testing.T, want ...string) []string {
	t.Helper()
	var got []string
	for {
		missing := false
		for _, w := range want {
			if !slices.Contains(got, w) {
				missing = true
			}
		}
		if !missing {
			return got
		}
		m := c.Next(t)
		sub, ok := m.(*webproto.Sub)
		if !ok {
			t.Fatalf("page sent %s while %v were not asked for yet", show(m), want)
		}
		got = append(got, sub.Topics...)
	}
}

// WaitSub reads messages until a sub that names topic; anything but a
// sub on the way fails the test.
func (c *HubConn) WaitSub(t *testing.T, topic string) {
	t.Helper()
	for {
		m := c.Next(t)
		sub, ok := m.(*webproto.Sub)
		if !ok {
			t.Fatalf("page sent %s while a sub for %s was expected", show(m), topic)
		}
		if slices.Contains(sub.Topics, topic) {
			return
		}
	}
}
