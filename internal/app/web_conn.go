package app

import (
	"bufio"
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// One hub connection (WEB-6): a browser through the gateway, or the
// gateway's control connection. A reader goroutine decodes the lines and
// hands every call to Update; a writer goroutine writes the outbound
// queue with a deadline, so a stalled gateway never blocks the UI.

// webWriteTimeout: a write that stalls this long closes the connection
// (the gateway then closes the WebSocket with 4008).
const webWriteTimeout = 5 * time.Second

// webConn is one hub connection.
type webConn struct {
	hub *webHub
	nc  net.Conn

	id     string // the gateway's connection id; "ctl-N" for a control connection
	role   webproto.Role
	device string
	name   string

	ctx    context.Context // ends when the connection or the hub ends
	cancel context.CancelFunc

	out        *webQueue
	writerDone chan struct{}

	hello  bool   // reader goroutine only: hello was seen
	lastID uint64 // reader goroutine only: the last request id

	inflight atomic.Int32 // requests sent to Update and not answered yet

	subs map[string]*webSub // Update only: live subscriptions by topic
}

func newWebConn(h *webHub, nc net.Conn) *webConn {
	ctx, cancel := context.WithCancel(h.ctx)
	return &webConn{hub: h, nc: nc, ctx: ctx, cancel: cancel, out: newWebQueue(), writerDone: make(chan struct{}), subs: map[string]*webSub{}}
}

// run serves the connection until EOF, an error or a protocol error. The
// writer flushes what is queued (bounded by its deadline) before the
// connection is closed.
func (c *webConn) run(br *bufio.Reader) {
	go c.writeLoop()
	defer func() {
		c.cancel()
		c.out.close()
		<-c.writerDone
	}()
	for {
		line, err := webReadLine(br)
		if err != nil {
			return
		}
		if !c.dispatch(line) {
			return
		}
	}
}

// dispatch handles one line on the reader goroutine; false ends the
// connection. Nothing here touches the Model: what needs it goes to
// Update as a message.
func (c *webConn) dispatch(line []byte) bool {
	msg, err := webproto.DecodeClient(line)
	if err != nil {
		return c.protocolError("That message is not valid.")
	}
	if c.role == webproto.RoleCtl { // the gateway's control connection
		switch msg := msg.(type) {
		case *webproto.Req:
			return c.request(msg)
		case *webproto.Ping:
			return c.send(&webproto.Pong{T: webproto.KindPong, N: msg.N})
		case *webproto.Bye:
			return false
		}
		return c.protocolError("The control connection does not take that message.")
	}
	if h, ok := msg.(*webproto.Hello); ok {
		if c.hello {
			return c.protocolError("hello was already sent.")
		}
		if h.V != webproto.Version {
			return c.protocolError("This page does not match this termocode. Reload it.")
		}
		c.hello = true
		// The token stays in the gateway's hands: it is not kept here.
		return c.hub.send(c.ctx, webHelloMsg{hub: c.hub, conn: c, resume: h.Resume}) == nil
	}
	if !c.hello {
		return c.protocolError("Say hello first.")
	}
	switch msg := msg.(type) {
	case *webproto.Ping:
		return c.send(&webproto.Pong{T: webproto.KindPong, N: msg.N})
	case *webproto.Bye:
		return false
	case *webproto.Req:
		return c.request(msg)
	case *webproto.Sub:
		return c.hub.send(c.ctx, webSubMsg{hub: c.hub, conn: c, topics: msg.Topics}) == nil
	case *webproto.Unsub:
		return c.hub.send(c.ctx, webSubMsg{hub: c.hub, conn: c, topics: msg.Topics, unsub: true}) == nil
	}
	return c.protocolError("That message is not valid here.")
}

// send queues one message not tied to a topic; false once the
// connection is closing. Such messages pass the soft caps (an answer is
// never dropped); a queue over its hard cap closes the connection.
func (c *webConn) send(m webproto.Msg) bool {
	b, err := webproto.Encode(m)
	if err != nil {
		recordError("[web] encode " + m.Type() + ": " + err.Error())
		return false
	}
	switch c.out.push(b) {
	case webQueued:
		return true
	case webFull:
		c.tooSlow()
	}
	return false
}

// tooSlow closes a connection whose gateway does not keep up; the
// gateway closes the browser with 4008.
func (c *webConn) tooSlow() {
	recordError("[web] connection " + c.id + " too slow: closed")
	c.out.close()
	_ = c.nc.Close()
}

// protocolError sends one err line and ends the connection (the writer
// flushes it first).
func (c *webConn) protocolError(msg string) bool {
	c.send(&webproto.ErrMsg{T: webproto.KindErr, Code: webproto.CodeBadRequest, Msg: msg})
	return false
}

// writeLoop writes queued frames, one line each, until the queue closes.
func (c *webConn) writeLoop() {
	defer close(c.writerDone)
	bw := bufio.NewWriterSize(c.nc, 64<<10)
	for {
		frames, ok := c.out.take()
		if !ok {
			return
		}
		_ = c.nc.SetWriteDeadline(time.Now().Add(webWriteTimeout))
		for _, f := range frames {
			_, _ = bw.Write(f)
			_ = bw.WriteByte('\n')
		}
		if err := bw.Flush(); err != nil {
			_ = c.nc.Close() // the reader sees the close and ends the connection
			c.out.close()
			return
		}
	}
}
