package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// rig is one relay on 127.0.0.1 (httptest listens on loopback only) and
// the events it logged.
type rig struct {
	t   *testing.T
	srv *Server
	hs  *httptest.Server

	mu     sync.Mutex
	events []Event
}

func newRig(t *testing.T, cfg Config) *rig {
	t.Helper()
	r := &rig{t: t}
	cfg.Log = func(e Event) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
	}
	r.srv = New(cfg)
	r.hs = httptest.NewServer(r.srv)
	t.Cleanup(func() {
		r.srv.Close()
		r.hs.Close()
	})
	return r
}

func (r *rig) url(room, side string) string {
	return "ws" + strings.TrimPrefix(r.hs.URL, "http") + PathPrefix + room + "?side=" + side
}

func (r *rig) dial(ctx context.Context, room, side string) (*websocket.Conn, *http.Response, error) {
	c, resp, err := websocket.Dial(ctx, r.url(room, side), nil)
	if err == nil {
		c.SetReadLimit(1 << 20)
	}
	return c, resp, err
}

func (r *rig) mustDial(ctx context.Context, room, side string) *websocket.Conn {
	r.t.Helper()
	c, _, err := r.dial(ctx, room, side)
	if err != nil {
		r.t.Fatalf("dial %s: %v", side, err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func (r *rig) logged() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// pair dials a gateway and a page into room and waits for the paired note.
func (r *rig) pair(ctx context.Context, room string) (gw, page *websocket.Conn) {
	r.t.Helper()
	gw = r.mustDial(ctx, room, SideGateway)
	waitWaiting(r.t, r.srv, room, 1)
	page = r.mustDial(ctx, room, SidePage)
	typ, b, err := gw.Read(ctx)
	if err != nil || typ != websocket.MessageText || string(b) != MsgPaired {
		r.t.Fatalf("the gateway's first message = %v %q %v, want the text %q", typ, b, err, MsgPaired)
	}
	return gw, page
}

func waitWaiting(t *testing.T, s *Server, room string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.Waiting(room) != n {
		if time.Now().After(deadline) {
			t.Fatalf("waiting gateways in the room = %d, want %d", s.Waiting(room), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func closeCode(err error) websocket.StatusCode { return websocket.CloseStatus(err) }

func testRoom(t *testing.T) string {
	t.Helper()
	id, err := NewRoomID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNewRoomIDIs128RandomBits(t *testing.T) {
	a, b := testRoom(t), testRoom(t)
	if !ValidRoom(a) || len(a) != 22 || a == b {
		t.Fatalf("room ids %q %q: want two different 22-char base64url ids", a, b)
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 23), strings.Repeat("a", 21) + "/", strings.Repeat("a", 21) + "="} {
		if ValidRoom(bad) {
			t.Errorf("ValidRoom(%q) = true", bad)
		}
	}
}

func TestRelayCopiesBytesBothWaysUnchanged(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	room := testRoom(t)
	gw, page := r.pair(ctx, room)
	up := []byte{0, 1, 2, 0xff, 'h', 'i'}
	if err := page.Write(ctx, websocket.MessageBinary, up); err != nil {
		t.Fatal(err)
	}
	typ, got, err := gw.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || !bytes.Equal(got, up) {
		t.Fatalf("gateway got %v %x %v, want %x", typ, got, err, up)
	}
	down := bytes.Repeat([]byte{7}, 60000)
	if err := gw.Write(ctx, websocket.MessageBinary, down); err != nil {
		t.Fatal(err)
	}
	typ, got, err = page.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || !bytes.Equal(got, down) {
		t.Fatalf("page got %v len %d %v, want %d bytes", typ, len(got), err, len(down))
	}
}

func TestRelayLogsNoPayloadAndNoRoomID(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	room := testRoom(t)
	gw, page := r.pair(ctx, room)
	marker := []byte("SECRET-MARKER-payload")
	_ = page.Write(ctx, websocket.MessageBinary, marker)
	_, _, _ = gw.Read(ctx)
	_ = page.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(5 * time.Second)
	for {
		done := false
		for _, e := range r.logged() {
			if e.Kind == EvEnd {
				done = true
			}
		}
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	all, _ := json.Marshal(r.logged())
	if bytes.Contains(all, marker) || bytes.Contains(all, []byte(room)) {
		t.Fatalf("the log holds the payload or the room id: %s", all)
	}
	var end *Event
	for _, e := range r.logged() {
		if e.Kind == EvEnd {
			e := e
			end = &e
		}
	}
	if end == nil || end.Up != int64(len(marker)) || end.Room == "" || len(end.Room) != 16 {
		t.Fatalf("end event = %+v, want the byte count %d and a 16-hex room hash", end, len(marker))
	}
}

func TestRelayRefusesABadRoomOrSide(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, u := range []string{
		r.url("not-a-room", SidePage),
		r.url(testRoom(t), "other"),
		"ws" + strings.TrimPrefix(r.hs.URL, "http") + "/elsewhere",
	} {
		c, resp, err := websocket.Dial(ctx, u, nil)
		if err == nil {
			_ = c.CloseNow()
			t.Errorf("%s: dial worked, want a refusal", u)
			continue
		}
		if resp == nil || (resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound) {
			t.Errorf("%s: %v, want 400 or 404", u, resp)
		}
	}
}

func TestRelayClosesAPageNobodyWaitsFor(t *testing.T) {
	r := newRig(t, Config{PageWait: 100 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page := r.mustDial(ctx, testRoom(t), SidePage)
	_, _, err := page.Read(ctx)
	if closeCode(err) != CloseNoPeer {
		t.Fatalf("read = %v, want close %d (no laptop)", err, CloseNoPeer)
	}
}

func TestRelayClosesBothOnATooBigMessage(t *testing.T) {
	r := newRig(t, Config{MaxMessage: 1024})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	_ = page.Write(ctx, websocket.MessageBinary, make([]byte, 2048))
	if _, _, err := gw.Read(ctx); err == nil {
		t.Fatal("the gateway got a message over the limit")
	}
	if _, _, err := page.Read(ctx); closeCode(err) != websocket.StatusMessageTooBig {
		t.Fatalf("page read = %v, want close 1009", err)
	}
}

func TestRelayClosesBothOnAText(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	_ = page.Write(ctx, websocket.MessageText, []byte("plain"))
	if _, _, err := gw.Read(ctx); err == nil {
		t.Fatal("a text message was copied")
	}
	if _, _, err := page.Read(ctx); closeCode(err) != websocket.StatusPolicyViolation {
		t.Fatalf("page read = %v, want close 1008", err)
	}
}

func TestRelayClosesAnIdlePair(t *testing.T) {
	r := newRig(t, Config{Idle: 150 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	start := time.Now()
	_, _, err := page.Read(ctx)
	if closeCode(err) != CloseIdle {
		t.Fatalf("page read = %v, want close %d", err, CloseIdle)
	}
	if _, _, err := gw.Read(ctx); closeCode(err) != CloseIdle {
		t.Fatalf("gateway read = %v, want close %d", err, CloseIdle)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("idle close took %v", time.Since(start))
	}
}

func TestRelayEndsAPairAtMaxLife(t *testing.T) {
	r := newRig(t, Config{MaxLife: 200 * time.Millisecond, Idle: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	go func() {
		for ctx.Err() == nil {
			_ = page.Write(ctx, websocket.MessageBinary, []byte{1})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for {
		_, _, err := gw.Read(ctx)
		if err != nil {
			if closeCode(err) != CloseMaxLife {
				t.Fatalf("gateway read = %v, want close %d", err, CloseMaxLife)
			}
			return
		}
	}
}

func TestRelayLimitsConnectionsPerAddress(t *testing.T) {
	r := newRig(t, Config{MaxConnsPerIP: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	room := testRoom(t)
	r.mustDial(ctx, room, SideGateway)
	r.mustDial(ctx, testRoom(t), SideGateway)
	c, resp, err := r.dial(ctx, testRoom(t), SideGateway)
	if err == nil {
		_ = c.CloseNow()
		t.Fatal("a third connection from one address got in")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third dial = %v, want 429", resp)
	}
}

func TestRelayLimitsWaitingGatewaysPerRoom(t *testing.T) {
	r := newRig(t, Config{MaxWaiting: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	room := testRoom(t)
	r.mustDial(ctx, room, SideGateway)
	waitWaiting(t, r.srv, room, 1)
	c := r.mustDial(ctx, room, SideGateway)
	if _, _, err := c.Read(ctx); closeCode(err) != CloseLimit {
		t.Fatalf("second waiting gateway: %v, want close %d", err, CloseLimit)
	}
}

func TestRelayPairsEachPageWithItsOwnGateway(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	room := testRoom(t)
	gw1, page1 := r.pair(ctx, room)
	gw2, page2 := r.pair(ctx, room)
	_ = page1.Write(ctx, websocket.MessageBinary, []byte("one"))
	_ = page2.Write(ctx, websocket.MessageBinary, []byte("two"))
	if _, b, _ := gw1.Read(ctx); string(b) != "one" {
		t.Fatalf("gw1 got %q", b)
	}
	if _, b, _ := gw2.Read(ctx); string(b) != "two" {
		t.Fatalf("gw2 got %q", b)
	}
	if r.srv.Pairs(room) != 2 {
		t.Fatalf("pairs = %d, want 2", r.srv.Pairs(room))
	}
}

func TestRelayLimitsPairsPerRoom(t *testing.T) {
	r := newRig(t, Config{MaxPairs: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	room := testRoom(t)
	r.pair(ctx, room)
	r.mustDial(ctx, room, SideGateway)
	waitWaiting(t, r.srv, room, 1)
	page := r.mustDial(ctx, room, SidePage)
	if _, _, err := page.Read(ctx); closeCode(err) != CloseLimit {
		t.Fatalf("second page: %v, want close %d", err, CloseLimit)
	}
}

func TestRelayPacesTheBytes(t *testing.T) {
	r := newRig(t, Config{BytesPerSecond: 100_000, MaxMessage: 20_000})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	msg := make([]byte, 20_000)
	start := time.Now()
	go func() {
		for i := 0; i < 7; i++ {
			_ = page.Write(ctx, websocket.MessageBinary, msg)
		}
	}()
	for i := 0; i < 7; i++ {
		if _, _, err := gw.Read(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// 140 kB at 100 kB/s with a 20 kB burst: at least 1.2 s.
	if el := time.Since(start); el < time.Second {
		t.Fatalf("140 kB went through in %v, want the 100 kB/s pace", el)
	}
}

func TestRelayCloseEndsEveryConnection(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gw, page := r.pair(ctx, testRoom(t))
	waiting := r.mustDial(ctx, testRoom(t), SideGateway)
	errs := make(chan error, 3)
	for _, c := range []*websocket.Conn{gw, page, waiting} {
		go func() {
			_, _, err := c.Read(ctx)
			errs <- err
		}()
	}
	r.srv.Close()
	for range 3 {
		if err := <-errs; err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("read after Close = %v, want a close", err)
		}
	}
}

func TestRelayForgetsAWaitingGatewayThatWentAway(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	room := testRoom(t)
	gw := r.mustDial(ctx, room, SideGateway)
	waitWaiting(t, r.srv, room, 1)
	_ = gw.CloseNow()
	waitWaiting(t, r.srv, room, 0)
}

// A connection that comes once the relay is closing is refused with 503
// before it is upgraded: Close's wait never races a new connection.
func TestRelayRefusesConnectionsOnceClosed(t *testing.T) {
	r := newRig(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.srv.Close()
	c, resp, err := r.dial(ctx, testRoom(t), SideGateway)
	if c != nil {
		_ = c.CloseNow()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("dial after Close: err %v, status %d, want 503", err, code)
	}
}
