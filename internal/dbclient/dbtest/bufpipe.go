package dbtest

import (
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// BufferedPipe is net.Pipe with a buffer in each direction: a write never
// waits for the reader. The SSH handshake needs it (both sides write
// their key exchange packets before reading); nothing listens on a port.
func BufferedPipe() (net.Conn, net.Conn) {
	ab, ba := newPipeBuf(), newPipeBuf()
	return &bufConn{rx: ba, tx: ab, name: "pipe-a"}, &bufConn{rx: ab, tx: ba, name: "pipe-b"}
}

type pipeBuf struct {
	mu     sync.Mutex
	cond   *sync.Cond
	data   []byte
	closed bool
}

func newPipeBuf() *pipeBuf {
	b := &pipeBuf{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *pipeBuf) close() {
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

type bufConn struct {
	rx, tx *pipeBuf
	name   string

	dmu      sync.Mutex
	deadline time.Time
	timer    *time.Timer
}

func (c *bufConn) Read(p []byte) (int, error) {
	c.rx.mu.Lock()
	defer c.rx.mu.Unlock()
	for len(c.rx.data) == 0 {
		if c.rx.closed {
			return 0, io.EOF
		}
		if c.expired() {
			return 0, os.ErrDeadlineExceeded
		}
		c.rx.cond.Wait()
	}
	n := copy(p, c.rx.data)
	c.rx.data = c.rx.data[n:]
	return n, nil
}

func (c *bufConn) Write(p []byte) (int, error) {
	c.tx.mu.Lock()
	defer c.tx.mu.Unlock()
	if c.tx.closed {
		return 0, io.ErrClosedPipe
	}
	c.tx.data = append(c.tx.data, p...)
	c.tx.cond.Broadcast()
	return len(p), nil
}

func (c *bufConn) Close() error {
	c.rx.close()
	c.tx.close()
	c.dmu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.dmu.Unlock()
	return nil
}

func (c *bufConn) expired() bool {
	c.dmu.Lock()
	defer c.dmu.Unlock()
	return !c.deadline.IsZero() && !time.Now().Before(c.deadline)
}

func (c *bufConn) LocalAddr() net.Addr  { return pipeAddr(c.name) }
func (c *bufConn) RemoteAddr() net.Addr { return pipeAddr(c.name) }

func (c *bufConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }
func (c *bufConn) SetReadDeadline(t time.Time) error {
	c.dmu.Lock()
	defer c.dmu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.deadline = t
	if !t.IsZero() {
		c.timer = time.AfterFunc(time.Until(t), func() {
			c.rx.mu.Lock()
			c.rx.cond.Broadcast()
			c.rx.mu.Unlock()
		})
	}
	return nil
}
func (c *bufConn) SetWriteDeadline(time.Time) error { return nil }
