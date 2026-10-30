package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The web hub (ADR 0020, WEB-6): the UI process's end of the web UI. It
// listens on <key>.web.sock in the private session dir; the web gateway
// process opens one connection per browser (first line "open") and one
// control connection (first line "ctl"). Every connection passes the
// same-uid peer check before a byte is read, and at most webMaxConns are
// served at once.
//
// The hub never touches the Model from its goroutines: a request becomes
// a webCallMsg that runs on Update (web_call.go), like an MCP call. The
// listener runs only while the web side is on; nothing listens by
// default.

// webMaxConns caps the hub connections served at once; more are closed.
const webMaxConns = 16

// webFirstLineWait is how long a new connection may take to send its
// first line before it is closed (a silent one must not hold a slot).
var webFirstLineWait atomic.Int64

func init() { webFirstLineWait.Store(int64(5 * time.Second)) }

// errWebHubStopped: the hub stopped while a connection or call waited.
var errWebHubStopped = errors.New("the web hub stopped")

// webHub links the connection goroutines to Update.
type webHub struct {
	ch     chan tea.Msg
	done   chan struct{}
	once   sync.Once
	ctx    context.Context // ends when the hub stops; every call's ctx comes from it
	cancel context.CancelFunc
	ops    map[string]webOpFunc   // Update only (tests swap it)
	topics map[string]webTopicDef // Update only (tests swap it)
	live   map[*webConn]bool      // Update only: browsers that said hello

	ln    net.Listener
	peer  func(net.Conn) error
	onErr func(error)

	mu     sync.Mutex
	all    map[net.Conn]*webConn // accepted; nil until the first line was read
	byID   map[string]*webConn   // browser connections by the gateway's id
	ctlSeq int
	closed bool
	wg     sync.WaitGroup
}

func newWebHub() *webHub {
	ctx, cancel := context.WithCancel(context.Background())
	return &webHub{
		ch: make(chan tea.Msg, 64), done: make(chan struct{}), ctx: ctx, cancel: cancel, ops: webOps, topics: webTopics,
		all: map[net.Conn]*webConn{}, byID: map[string]*webConn{}, live: map[*webConn]bool{},
	}
}

// serve accepts connections on ln in the background. peer must accept a
// connection before anything is read from it (nil refuses all). onErr
// hears refused connections; it runs on a hub goroutine.
func (h *webHub) serve(ln net.Listener, peer func(net.Conn) error, onErr func(error)) {
	h.mu.Lock()
	h.ln, h.peer, h.onErr = ln, peer, onErr
	h.mu.Unlock()
	h.wg.Add(1)
	go h.accept(ln)
}

// stop closes the listener (a unix listener removes its socket file) and
// every connection, ends every call's context and waits for the hub's
// goroutines. Callers waiting on Update get errWebHubStopped.
func (h *webHub) stop() {
	h.once.Do(func() {
		close(h.done)
		h.cancel()
		h.mu.Lock()
		h.closed = true
		if h.ln != nil {
			_ = h.ln.Close()
		}
		for nc := range h.all {
			_ = nc.Close()
		}
		h.mu.Unlock()
		h.wg.Wait()
	})
}

func (h *webHub) report(err error) {
	if h.onErr != nil {
		h.onErr(err)
	}
}

// send delivers msg to Update unless the hub stopped or ctx ended.
func (h *webHub) send(ctx context.Context, msg tea.Msg) error {
	select {
	case h.ch <- msg:
		return nil
	case <-h.done:
		return errWebHubStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

// browsers is how many browser connections are open (not the control
// connection).
func (h *webHub) browsers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byID)
}

// conns is how many connections are open now (also those still before
// their first line).
func (h *webHub) conns() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.all)
}

// ready is how many connections sent a good first line.
func (h *webHub) ready() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.all {
		if c != nil {
			n++
		}
	}
	return n
}

func (h *webHub) accept(ln net.Listener) {
	defer h.wg.Done()
	for {
		nc, err := ln.Accept()
		if err != nil {
			h.mu.Lock()
			closed := h.closed
			h.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return
			}
			h.report(err)
			select { // EMFILE and friends: do not spin
			case <-h.done:
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		h.mu.Lock()
		if h.closed || len(h.all) >= webMaxConns {
			h.mu.Unlock()
			_ = nc.Close()
			continue
		}
		h.all[nc] = nil
		h.wg.Add(1)
		h.mu.Unlock()
		go h.handle(nc)
	}
}

// handle serves one connection: peer check, first line, then the
// connection's own loop (web_conn.go).
func (h *webHub) handle(nc net.Conn) {
	defer h.wg.Done()
	var c *webConn
	defer func() { h.drop(nc, c) }()
	if h.peer == nil {
		h.report(errors.New("web hub: connection refused: no peer check"))
		return
	}
	if err := h.peer(nc); err != nil { // before the first read
		h.report(fmt.Errorf("web hub: connection refused: %w", err))
		return
	}
	_ = nc.SetReadDeadline(time.Now().Add(time.Duration(webFirstLineWait.Load())))
	br := bufio.NewReaderSize(nc, 64<<10)
	line, err := webReadLine(br)
	if err != nil {
		return
	}
	first, err := webproto.DecodeGatewayLine(line)
	if err != nil {
		h.report(fmt.Errorf("web hub: bad first line: %w", err))
		return
	}
	_ = nc.SetReadDeadline(time.Time{})
	if c, err = h.admit(nc, first); err != nil {
		h.report(fmt.Errorf("web hub: %w", err))
		return
	}
	c.run(br)
	if c.role != webproto.RoleCtl { // Update forgets it (and keeps its topics for a resume)
		_ = h.send(h.ctx, webGoneMsg{hub: h, conn: c})
	}
}

// admit registers a connection after its first line.
func (h *webHub) admit(nc net.Conn, first webproto.Msg) (*webConn, error) {
	c := newWebConn(h, nc)
	switch f := first.(type) {
	case *webproto.Open:
		c.id, c.role, c.device, c.name = f.Conn, f.Role, f.Device, f.Name
	case *webproto.Ctl:
		c.role = webproto.RoleCtl
	default:
		return nil, fmt.Errorf("first line %q", first.Type())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errWebHubStopped
	}
	if c.role == webproto.RoleCtl {
		h.ctlSeq++
		c.id = fmt.Sprintf("ctl-%d", h.ctlSeq)
	} else {
		if _, dup := h.byID[c.id]; dup {
			return nil, fmt.Errorf("connection id %q is in use", c.id)
		}
		h.byID[c.id] = c
	}
	h.all[nc] = c
	return c, nil
}

// drop forgets a connection and closes it.
func (h *webHub) drop(nc net.Conn, c *webConn) {
	h.mu.Lock()
	delete(h.all, nc)
	if c != nil && h.byID[c.id] == c {
		delete(h.byID, c.id)
	}
	h.mu.Unlock()
	_ = nc.Close()
}

// errWebLineTooLong: a line over the frame limit.
var errWebLineTooLong = errors.New("web hub: line too long")

// webReadLine reads one newline-framed line (without the newline), at
// most webproto.MaxFrame bytes. The result never aliases br's buffer.
func webReadLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		part, err := br.ReadSlice('\n')
		if len(buf)+len(part) > webproto.MaxFrame+1 {
			return nil, errWebLineTooLong
		}
		buf = append(buf, part...)
		switch {
		case err == nil:
			return buf[:len(buf)-1], nil
		case !errors.Is(err, bufio.ErrBufferFull):
			return nil, err
		}
	}
}
