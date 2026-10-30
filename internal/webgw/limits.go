//go:build unix

package webgw

import (
	"net"
	"sync"
)

// Limits are the HTTP server's limits against slow or many connections.
type Limits struct {
	// MaxConns caps the open connections (HTTP and WebSocket together).
	MaxConns int
}

// DefaultLimits are the gateway's limits.
var DefaultLimits = Limits{
	MaxConns: 32,
}

// LimitConns lets at most max connections be open at once. One more is
// closed right after accept, before anything is read, so a flood cannot
// queue up behind the cap; Accept goes on waiting for the next one.
func LimitConns(ln net.Listener, max int) net.Listener {
	return &connLimiter{Listener: ln, max: max}
}

type connLimiter struct {
	net.Listener
	max int

	mu sync.Mutex
	n  int
}

func (l *connLimiter) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if l.n >= l.max {
			l.mu.Unlock()
			_ = c.Close()
			continue
		}
		l.n++
		l.mu.Unlock()
		return &limitedConn{Conn: c, l: l}, nil
	}
}

// open is the number of connections handed out and not closed yet.
func (l *connLimiter) open() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

func (l *connLimiter) release() {
	l.mu.Lock()
	l.n--
	l.mu.Unlock()
}

// limitedConn frees its slot on the first Close only.
type limitedConn struct {
	net.Conn
	l    *connLimiter
	once sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.l.release)
	return err
}
