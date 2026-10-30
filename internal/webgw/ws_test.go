//go:build unix

package webgw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// fakeHub stands in for the hub: a TCP listener whose connections the
// test reads and writes line by line.
type fakeHub struct {
	t     *testing.T
	ln    net.Listener
	conns chan *hubPeer

	mu   sync.Mutex
	down bool // refuse new connections
}

type hubPeer struct {
	nc    net.Conn
	lines chan string
	first *string // the line next() saw
}

func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHub{t: t, ln: ln, conns: make(chan *hubPeer, 64)}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			p := &hubPeer{nc: nc, lines: make(chan string, 256)}
			go func() {
				defer close(p.lines)
				br := bufio.NewReader(nc)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					p.lines <- strings.TrimSuffix(line, "\n")
				}
			}()
			h.conns <- p
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return h
}

func (h *fakeHub) dial(ctx context.Context) (net.Conn, error) {
	h.mu.Lock()
	down := h.down
	h.mu.Unlock()
	if down {
		return nil, errors.New("hub is down")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp4", h.ln.Addr().String())
}

func (h *fakeHub) setDown() {
	h.mu.Lock()
	h.down = true
	h.mu.Unlock()
}

// next waits for the next hub connection with a first line; probes that
// send nothing are skipped.
func (h *fakeHub) next() *hubPeer {
	h.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case p := <-h.conns:
			select {
			case l, ok := <-p.lines:
				if !ok {
					continue
				}
				p.first = &l
				return p
			case <-timeout:
				h.t.Fatal("no hub line")
			}
		case <-timeout:
			h.t.Fatal("no hub connection")
			return nil
		}
	}
}

// none checks that no hub connection carrying a line came in.
func (h *fakeHub) none() {
	h.t.Helper()
	for {
		select {
		case p := <-h.conns:
			select {
			case l, ok := <-p.lines:
				if ok {
					h.t.Fatalf("the hub got a line: %s", l)
				}
			case <-time.After(time.Second):
				h.t.Fatal("a hub connection stayed open")
			}
		default:
			return
		}
	}
}

func (p *hubPeer) line(t *testing.T) map[string]any {
	t.Helper()
	if p.first != nil {
		var m map[string]any
		if err := json.Unmarshal([]byte(*p.first), &m); err != nil {
			t.Fatal(err)
		}
		p.first = nil
		return m
	}
	select {
	case l, ok := <-p.lines:
		if !ok {
			t.Fatal("hub connection closed")
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("hub line %q: %v", l, err)
		}
		return m
	case <-time.After(10 * time.Second):
		t.Fatal("no hub line")
	}
	return nil
}

func (p *hubPeer) send(t *testing.T, line string) {
	t.Helper()
	if _, err := p.nc.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

// eof waits until the gateway closed this hub connection.
func (p *hubPeer) eof(t *testing.T) {
	t.Helper()
	for {
		select {
		case _, ok := <-p.lines:
			if !ok {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("hub connection not closed")
		}
	}
}

// wsRig is a gateway HTTP server with /ws, a fake hub and real tokens.
type wsRig struct {
	t      *testing.T
	addr   string
	site   Site
	ws     *WS
	tokens *Tokens
	hub    *fakeHub
	clock  *fakeClock
	audit  *Audit
	alog   string
}

func newWSRig(t *testing.T, limits Limits, mod func(*WSConfig)) *wsRig {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	site := LoopbackSite(ln.Addr().(*net.TCPAddr).Port)
	alog := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := OpenAudit(alog, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &wsRig{t: t, site: site, tokens: NewTokens(DefaultIdle, DefaultMax), hub: newFakeHub(t),
		clock: &fakeClock{now: time.Now()}, audit: audit, alog: alog}
	cfg := WSConfig{Site: site, Tokens: r.tokens, Audit: audit, Dial: r.hub.dial, Now: r.clock.Now}
	if mod != nil {
		mod(&cfg)
	}
	r.ws = NewWS(cfg)
	mux := http.NewServeMux()
	mux.Handle("/ws", r.ws)
	srv := NewServer(site, mux, limits, nil)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		r.ws.CloseAll(webproto.CloseStopped, 5*time.Second)
		_ = srv.Close()
		r.ws.Wait()
		_ = audit.Close()
	})
	r.addr = ln.Addr().String()
	return r
}

func (r *wsRig) token() string {
	r.t.Helper()
	tok, _, err := r.tokens.Issue("loc-test", RoleControl, r.clock.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return tok
}

// dialOpts are what a browser on the page sends.
func (r *wsRig) dialOpts() *websocket.DialOptions {
	h := http.Header{}
	h.Set("Origin", "http://"+r.addr)
	return &websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{Subprotocol}}
}

func (r *wsRig) dial(opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws://"+r.addr+"/ws", opts)
}

func (r *wsRig) mustDial() *websocket.Conn {
	r.t.Helper()
	r.token() // the upgrade needs a live session
	c, _, err := r.dial(r.dialOpts())
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func writeText(t *testing.T, c *websocket.Conn, s string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func helloMsg(tok string) string {
	b, _ := json.Marshal(map[string]any{"t": "hello", "v": webproto.Version, "token": tok, "device_name": "Phone"})
	return string(b)
}

// connect dials, says hello and returns the browser and its hub peer
// after the open line and the hello reached the hub.
func (r *wsRig) connect() (*websocket.Conn, *hubPeer) {
	r.t.Helper()
	return r.connectWith(r.token())
}

// connectWith is connect with a given token.
func (r *wsRig) connectWith(tok string) (*websocket.Conn, *hubPeer) {
	r.t.Helper()
	c := r.mustDial()
	writeText(r.t, c, helloMsg(tok))
	p := r.hub.next()
	if open := p.line(r.t); open["t"] != "open" {
		r.t.Fatalf("first hub line %v", open)
	}
	if h := p.line(r.t); h["t"] != "hello" {
		r.t.Fatalf("second hub line %v", h)
	}
	return c, p
}

// closeCode reads until the connection ends and returns its close code.
func closeCode(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				t.Fatal("connection not closed")
			}
			return websocket.CloseStatus(err)
		}
	}
}

// readMsg reads one text message from the gateway as a map.
func readMsg(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("type %v", typ)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func auditText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// waitFor polls cond until it holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Spec (never-do 11): permessage-deflate is never negotiated, even when
// the browser offers it; the subprotocol is termocode.v2.
func TestNoCompressionNegotiated(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	r.token()
	opts := r.dialOpts()
	opts.CompressionMode = websocket.CompressionContextTakeover
	c, resp, err := r.dial(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); ext != "" {
		t.Fatalf("extensions negotiated: %q", ext)
	}
	if c.Subprotocol() != Subprotocol {
		t.Fatalf("subprotocol %q", c.Subprotocol())
	}
}

// Spec (T2, 1.4): no upgrade without an exact Origin, the subprotocol and
// a live session; the refusal is plain HTTP, never a WebSocket.
func TestWSRefusedBeforeUpgrade(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	if _, resp, err := r.dial(r.dialOpts()); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: %v %v, want 401", err, resp)
	}
	r.token()
	cases := map[string]func(*websocket.DialOptions){
		"other origin":   func(o *websocket.DialOptions) { o.HTTPHeader.Set("Origin", "http://evil.example") },
		"no origin":      func(o *websocket.DialOptions) { o.HTTPHeader.Del("Origin") },
		"no subprotocol": func(o *websocket.DialOptions) { o.Subprotocols = nil },
		"old protocol":   func(o *websocket.DialOptions) { o.Subprotocols = []string{"termocode.v1"} },
	}
	for name, mod := range cases {
		opts := r.dialOpts()
		mod(opts)
		c, resp, err := r.dial(opts)
		if err == nil {
			c.CloseNow()
			t.Errorf("%s: upgraded", name)
			continue
		}
		if resp == nil || resp.StatusCode < 400 {
			t.Errorf("%s: %v, want a 4xx", name, resp)
		}
	}
	r.hub.none()
}

// Spec: the HTTP read and write deadlines end at the upgrade, so a
// WebSocket lives longer than the server's ReadTimeout.
func TestWSDeadlinesClearedAfterUpgrade(t *testing.T) {
	l := DefaultLimits
	l.ReadTimeout, l.WriteTimeout = 200*time.Millisecond, 200*time.Millisecond
	r := newWSRig(t, l, nil)
	c, p := r.connect()
	<-time.After(3 * l.ReadTimeout) // let the old deadlines pass
	writeText(t, c, `{"t":"ping","n":"1"}`)
	if m := p.line(t); m["t"] != "ping" {
		t.Fatalf("hub got %v", m)
	}
	p.send(t, `{"t":"pong","n":"1"}`)
	if m := readMsg(t, c); m["t"] != "pong" {
		t.Fatalf("browser got %v", m)
	}
}

// Spec (1.4): hello first, once, in time, v 2, with a live token.
func TestHelloRules(t *testing.T) {
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) { c.HelloWait = 300 * time.Millisecond })
	cases := []struct {
		name string
		send func(*websocket.Conn)
		want webproto.CloseCode
	}{
		{"not hello", func(c *websocket.Conn) { writeText(t, c, `{"t":"ping","n":"1"}`) }, webproto.ClosePolicy},
		{"bad json", func(c *websocket.Conn) { writeText(t, c, `{"t":"hello"`) }, webproto.ClosePolicy},
		{"late", func(*websocket.Conn) {}, webproto.ClosePolicy},
		{"binary", func(c *websocket.Conn) {
			_ = c.Write(context.Background(), websocket.MessageBinary, []byte(helloMsg(r.token())))
		}, webproto.CloseUnsupportedData},
		{"v1", func(c *websocket.Conn) {
			writeText(t, c, strings.Replace(helloMsg(r.token()), `"v":2`, `"v":1`, 1))
		}, webproto.CloseUnsupportedVersion},
		{"v3 with new fields", func(c *websocket.Conn) {
			writeText(t, c, `{"t":"hello","v":3,"token":"x","new":true}`)
		}, webproto.CloseUnsupportedVersion},
		{"unknown token", func(c *websocket.Conn) { writeText(t, c, helloMsg("sk-"+"test-not-a-token")) }, webproto.CloseAuthFailed},
		{"too big", func(c *websocket.Conn) {
			writeText(t, c, helloMsg(strings.Repeat("a", webproto.MaxHello)))
		}, webproto.CloseTooBig},
	}
	for _, tc := range cases {
		c := r.mustDial()
		tc.send(c)
		if got := closeCode(t, c); got != websocket.StatusCode(tc.want) {
			t.Errorf("%s: closed with %d, want %d", tc.name, got, tc.want)
		}
	}
	r.hub.none()

	// An expired token is 4002.
	tok := r.token()
	r.clock.Add(DefaultIdle + time.Second)
	c := r.mustDial()
	writeText(t, c, helloMsg(tok))
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseTokenExpired) {
		t.Fatalf("expired: %d, want 4002", got)
	}
	r.hub.none()
}

// Spec (1.4, WEB-8): the hub gets the "open" line, then the browser's
// hello with the token taken out; after that both ways pass unchanged.
func TestOpenLineAndHelloWithoutToken(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c := r.mustDial()
	tok := r.token()
	writeText(t, c, helloMsg(tok))
	p := r.hub.next()
	open := p.line(t)
	if open["t"] != "open" || open["role"] != "controller" || open["device"] != "loc-test" ||
		open["name"] != "Phone" || !strings.HasPrefix(open["conn"].(string), "c-") {
		t.Fatalf("open line %v", open)
	}
	hello := p.line(t)
	if hello["t"] != "hello" || hello["v"] != float64(webproto.Version) || hello["token"] != "" {
		t.Fatalf("hello to the hub %v", hello)
	}
	writeText(t, c, `{"t":"sub","topics":["status"]}`)
	if m := p.line(t); m["t"] != "sub" {
		t.Fatalf("hub got %v", m)
	}
	p.send(t, `{"t":"snap","topic":"status","seq":1,"data":{"x":1}}`)
	if m := readMsg(t, c); m["t"] != "snap" || m["topic"] != "status" {
		t.Fatalf("browser got %v", m)
	}
	if strings.Contains(auditText(t, r.alog), tok) {
		t.Fatal("token in the audit log")
	}
}

// Spec (1.4): after hello, a binary frame is 1003, a bad message or a
// second hello is 1008, a frame over the read limit is 1009.
func TestBadMessagesClose(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	cases := []struct {
		name string
		send func(*websocket.Conn)
		want webproto.CloseCode
	}{
		{"binary", func(c *websocket.Conn) {
			_ = c.Write(context.Background(), websocket.MessageBinary, []byte(`{"t":"ping","n":"1"}`))
		}, webproto.CloseUnsupportedData},
		{"unknown type", func(c *websocket.Conn) { writeText(t, c, `{"t":"input","d":"x"}`) }, webproto.ClosePolicy},
		{"unknown field", func(c *websocket.Conn) { writeText(t, c, `{"t":"ping","n":"1","x":1}`) }, webproto.ClosePolicy},
		{"hello twice", func(c *websocket.Conn) { writeText(t, c, helloMsg(r.token())) }, webproto.ClosePolicy},
		{"too big", func(c *websocket.Conn) {
			_ = c.Write(context.Background(), websocket.MessageText, make([]byte, webproto.MaxFrame+1))
		}, webproto.CloseTooBig},
	}
	for _, tc := range cases {
		c, p := r.connect()
		tc.send(c)
		if got := closeCode(t, c); got != websocket.StatusCode(tc.want) {
			t.Errorf("%s: closed with %d, want %d", tc.name, got, tc.want)
		}
		p.eof(t)
	}
	if !strings.Contains(auditText(t, r.alog), `"ev":"proto_error"`) {
		t.Fatal("no proto_error in the audit log")
	}
}

// Spec (1.5, 4.4): when the UI process goes away the browser gets 4012;
// when the hub drops only this connection (it was too slow) 4008; after
// an error for the connection 1008; a bye from the hub closes with its
// code.
func TestHubEndCloseCodes(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)

	c, p := r.connect()
	_ = p.nc.Close()
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseSlowClient) {
		t.Errorf("hub alive, conn dropped: %d, want 4008", got)
	}

	c, p = r.connect()
	p.send(t, `{"t":"err","code":"bad_request","msg":"Say hello first."}`)
	if m := readMsg(t, c); m["t"] != "err" {
		t.Fatalf("browser got %v", m)
	}
	_ = p.nc.Close()
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.ClosePolicy) {
		t.Errorf("after err: %d, want 1008", got)
	}

	c, p = r.connect()
	p.send(t, `{"t":"bye","code":4009}`)
	if m := readMsg(t, c); m["t"] != "bye" {
		t.Fatalf("browser got %v", m)
	}
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseStopped) {
		t.Errorf("bye: %d, want 4009", got)
	}
	p.eof(t)

	c, p = r.connect()
	r.hub.setDown()
	_ = p.nc.Close()
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseHubGone) {
		t.Errorf("hub gone: %d, want 4012", got)
	}

	// No hub at all at hello time: 4012 too.
	c = r.mustDial()
	writeText(t, c, helloMsg(r.token()))
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseHubGone) {
		t.Errorf("no hub: %d, want 4012", got)
	}
}

// Spec: the browser's bye goes to the hub and ends with 1000.
func TestBrowserByeCloses(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	c, p := r.connect()
	writeText(t, c, `{"t":"bye"}`)
	if m := p.line(t); m["t"] != "bye" {
		t.Fatalf("hub got %v", m)
	}
	p.eof(t)
	if got := closeCode(t, c); got != websocket.StatusNormalClosure {
		t.Fatalf("closed with %d", got)
	}
	waitFor(t, "disconnect logged", func() bool {
		return strings.Contains(auditText(t, r.alog), `"ev":"disconnect"`)
	})
	if !strings.Contains(auditText(t, r.alog), `"ev":"connect"`) {
		t.Fatal("no connect in the audit log")
	}
}

// Spec (owner limit): at most MaxViewers browsers at once; one more is
// closed with 4005 before the hub hears of it, and gets in once a seat
// is free.
func TestTooManyViewers4005(t *testing.T) {
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) { c.MaxViewers = 2 })
	c1, p1 := r.connect()
	r.connect()
	c := r.mustDial()
	writeText(t, c, helloMsg(r.token()))
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseTooManyViewers) {
		t.Fatalf("third browser: %d, want 4005", got)
	}
	r.hub.none()
	_ = c1.Close(websocket.StatusNormalClosure, "")
	p1.eof(t)
	waitFor(t, "a free seat", func() bool { return r.ws.Count() == 1 })
	r.connect()
}

// Spec (owner rule): when the audit log cannot be written, new browsers
// are refused (1011) and the hub never hears of them.
func TestAuditFailureFailsClosed(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	_ = r.audit.Close()
	c := r.mustDial()
	writeText(t, c, helloMsg(r.token()))
	if got := closeCode(t, c); got != websocket.StatusInternalError {
		t.Fatalf("closed with %d, want 1011", got)
	}
	r.hub.none()
}
