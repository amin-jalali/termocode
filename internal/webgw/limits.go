//go:build unix

package webgw

import (
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// Limits are the HTTP server's limits against slow or many connections
// (T17). The request body cap is MaxBodyBytes, in the guard.
type Limits struct {
	// ReadHeaderTimeout cuts off a client that does not finish its
	// headers (slowloris).
	ReadHeaderTimeout time.Duration
	// ReadTimeout and WriteTimeout bound one HTTP request. The WebSocket
	// handler clears both on its own connection after the upgrade.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// IdleTimeout closes a kept-alive connection with no request.
	IdleTimeout time.Duration
	// MaxHeaderBytes caps the request headers (net/http adds 4 KiB slack).
	MaxHeaderBytes int
	// MaxConns caps the open connections (HTTP and WebSocket together).
	MaxConns int
}

// DefaultLimits are the gateway's limits.
var DefaultLimits = Limits{
	ReadHeaderTimeout: 5 * time.Second,
	ReadTimeout:       10 * time.Second,
	WriteTimeout:      10 * time.Second,
	IdleTimeout:       60 * time.Second,
	MaxHeaderBytes:    8 << 10,
	MaxConns:          32,
}

// NewServer is the gateway's HTTP server: next behind the Guard for site,
// with the limits l. net/http's own error log is dropped: its lines can
// quote request data, and nothing from a request may reach a log. Serve it
// on a listener wrapped with LimitConns(ln, l.MaxConns).
func NewServer(site Site, next http.Handler, l Limits) *http.Server {
	return &http.Server{
		Handler:           Guard(site, next),
		ReadHeaderTimeout: l.ReadHeaderTimeout,
		ReadTimeout:       l.ReadTimeout,
		WriteTimeout:      l.WriteTimeout,
		IdleTimeout:       l.IdleTimeout,
		MaxHeaderBytes:    l.MaxHeaderBytes,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
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
