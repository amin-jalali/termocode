//go:build unix

package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// webRig is a hub on a real socket and a Model the test drives as Update.
type webRig struct {
	t    *testing.T
	m    *Model
	hub  *webHub
	path string
}

// newWebRig starts a hub on a short socket path with the given peer check
// (nil: the real same-uid check).
func newWebRig(t *testing.T, peer func(net.Conn) error) *webRig {
	t.Helper()
	m := aiTestModel(t)
	if peer == nil {
		peer = webHubPeer
	}
	path := filepath.Join(webTestDir(t), "k.web.sock")
	ln, err := webHubListen(path)
	if err != nil {
		t.Fatal(err)
	}
	hub := newWebHub()
	hub.serve(ln, peer, func(error) {})
	t.Cleanup(hub.stop)
	m.web = hub
	return &webRig{t: t, m: &m, hub: hub, path: path}
}

// pumpUntil plays Update for the hub until cond holds.
func (r *webRig) pumpUntil(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case msg := <-r.hub.ch:
			if _, ok := r.m.handleWebMsg(msg); !ok {
				r.t.Fatalf("not a web message: %T", msg)
			}
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			r.t.Fatal("timed out waiting for " + what)
		}
	}
}

// webClient is the gateway side of one hub connection.
type webClient struct {
	t     *testing.T
	c     net.Conn
	lines chan string // every line the hub wrote, then closed at EOF
}

// dial connects and, when first != "", sends it as the first line.
func (r *webRig) dial(first string) *webClient {
	r.t.Helper()
	c, err := net.Dial("unix", r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	wc := &webClient{t: r.t, c: c, lines: make(chan string, 1024)}
	go func() {
		defer close(wc.lines)
		br := bufio.NewReader(c)
		for {
			s, err := br.ReadString('\n')
			if err != nil {
				return
			}
			wc.lines <- s[:len(s)-1]
		}
	}()
	if first != "" {
		wc.send(first)
	}
	return wc
}

func (c *webClient) send(line string) {
	c.t.Helper()
	if _, err := c.c.Write([]byte(line + "\n")); err != nil {
		c.t.Fatal(err)
	}
}

// next waits (playing Update) for the next line from the hub; "" at EOF.
func (r *webRig) next(c *webClient) (string, bool) {
	r.t.Helper()
	var line string
	var ok, got bool
	r.pumpUntil("a line from the hub", func() bool {
		select {
		case line, ok = <-c.lines:
			got = true
		default:
		}
		return got
	})
	return line, ok
}

// nextMsg is next decoded into a map.
func (r *webRig) nextMsg(c *webClient) map[string]any {
	r.t.Helper()
	line, ok := r.next(c)
	if !ok {
		r.t.Fatal("connection closed")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		r.t.Fatalf("bad line %q: %v", line, err)
	}
	return v
}

// waitEOF waits (playing Update) until the hub closed the connection.
func (r *webRig) waitEOF(c *webClient) {
	r.t.Helper()
	for {
		if _, ok := r.next(c); !ok {
			return
		}
	}
}

const webTestOpen = `{"t":"open","conn":"c-1","role":"controller","device":"loc-1","name":"Phone"}`

// countConn counts reads, to prove nothing was read before the peer check.
type countConn struct {
	net.Conn
	reads *atomic.Int32
}

func (c countConn) Read(p []byte) (int, error) {
	c.reads.Add(1)
	return c.Conn.Read(p)
}

// countListener wraps every accepted connection in a countConn.
type countListener struct {
	net.Listener
	reads *atomic.Int32
}

func (l countListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return countConn{c, l.reads}, nil
}

// Spec (WEB-6, T38): a connection whose peer is another uid is closed
// before a single byte is read from it; the same uid is let in.
func TestHubPeerUID(t *testing.T) {
	m := aiTestModel(t)
	path := filepath.Join(webTestDir(t), "k.web.sock")
	ln, err := webHubListen(path)
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	var refused atomic.Int32
	hub := newWebHub()
	hub.serve(countListener{ln, &reads}, func(net.Conn) error {
		refused.Add(1)
		return errors.New("peer uid 4242 is not ours")
	}, func(error) {})
	t.Cleanup(hub.stop)
	m.web = hub
	r := &webRig{t: t, m: &m, hub: hub, path: path}
	c := r.dial("")
	// The hub may close before or while we write: the write may fail.
	_, _ = c.c.Write([]byte(webTestOpen + "\n" + `{"t":"hello","v":2,"token":"x"}` + "\n"))
	r.waitEOF(c)
	if refused.Load() != 1 || reads.Load() != 0 {
		t.Fatalf("refused %d, reads %d: want 1 refusal and no read", refused.Load(), reads.Load())
	}
	if n := hub.conns(); n != 0 {
		t.Fatalf("%d connections left", n)
	}

	// The real check lets our own uid in.
	ok := newWebRig(t, nil)
	c2 := ok.dial(webTestOpen)
	ok.pumpUntil("the connection", func() bool { return ok.hub.conns() == 1 })
	_ = c2
}

// Spec (WEB-6, T38): at most 16 connections at once; the 17th is closed
// at once; when one goes, a new one fits again.
func TestHubConnCap(t *testing.T) {
	r := newWebRig(t, nil)
	var cs []*webClient
	for i := 0; i < webMaxConns; i++ {
		cs = append(cs, r.dial(""))
	}
	r.pumpUntil("16 connections", func() bool { return r.hub.conns() == webMaxConns })
	extra := r.dial("")
	r.waitEOF(extra)
	_ = cs[0].c.Close()
	r.pumpUntil("a free slot", func() bool { return r.hub.conns() == webMaxConns-1 })
	r.dial("")
	r.pumpUntil("16 again", func() bool { return r.hub.conns() == webMaxConns })
}

// Spec (WEB-6): the first line is open or ctl, decoded strictly; anything
// else closes the connection.
func TestHubFirstLine(t *testing.T) {
	r := newWebRig(t, nil)
	for _, bad := range []string{
		`{"t":"hello","v":2,"token":"x"}`,
		`{"t":"open","conn":"c-1","role":"root","device":"d"}`,
		`{"t":"open","conn":"c-1","role":"controller","device":"d","extra":1}`,
		`not json`,
	} {
		c := r.dial(bad)
		r.waitEOF(c)
	}
	open := r.dial(webTestOpen)
	ctl := r.dial(`{"t":"ctl"}`)
	r.pumpUntil("open and ctl", func() bool { return r.hub.conns() == 2 && r.hub.ready() == 2 })
	// A second live connection with the same id is refused.
	dup := r.dial(webTestOpen)
	r.waitEOF(dup)
	_, _ = open, ctl
}

// Spec (WEB-6): a connection that never sends its first line is closed
// after the first-line timeout, so it cannot hold a slot.
func TestHubFirstLineTimeout(t *testing.T) {
	defer webSetFirstLineTimeout(50 * time.Millisecond)()
	r := newWebRig(t, nil)
	c := r.dial("")
	r.waitEOF(c)
}

// webSetFirstLineTimeout sets the first-line wait for one test.
func webSetFirstLineTimeout(d time.Duration) func() {
	old := webFirstLineWait.Swap(int64(d))
	return func() { webFirstLineWait.Store(old) }
}
