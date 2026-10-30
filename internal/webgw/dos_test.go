//go:build unix

package webgw

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startServer serves h on a fresh 127.0.0.1 listener wrapped by wrap.
func startServer(t *testing.T, srv *http.Server, wrap func(net.Listener) net.Listener) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if wrap != nil {
		ln = wrap(ln)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// rawGet sends one HTTP/1.1 GET and returns what came back before the
// connection closed (or the deadline hit).
func rawGet(addr string, timeout time.Duration) (string, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		return "", err
	}
	b, err := io.ReadAll(c)
	return string(b), err
}

func okServer() *http.Server {
	return &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})}
}

// TestConnCap32: with MaxConns open, one more connection is closed at once
// with nothing served; when one closes, a new one is served again (T17).
func TestConnCap32(t *testing.T) {
	if DefaultLimits.MaxConns != 32 {
		t.Fatalf("MaxConns = %d, want 32", DefaultLimits.MaxConns)
	}
	addr := startServer(t, okServer(), func(ln net.Listener) net.Listener {
		return LimitConns(ln, 32)
	})
	held := make([]net.Conn, 0, 32)
	for i := 0; i < 32; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	// The 33rd: accepted by the kernel, closed by the cap before any read.
	out, err := rawGet(addr, 10*time.Second)
	if strings.Contains(out, "200 OK") {
		t.Fatalf("connection over the cap was served: %q %v", out, err)
	}

	held[0].Close()
	held = held[1:]
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, _ := rawGet(addr, 10*time.Second)
		if strings.Contains(out, "200 OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a freed slot was never reused")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerLimitsExact(t *testing.T) {
	want := Limits{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
		MaxConns:          32,
	}
	if DefaultLimits != want {
		t.Fatalf("DefaultLimits = %+v, want %+v", DefaultLimits, want)
	}
	srv := NewServer(testSite, okServer().Handler, DefaultLimits)
	if srv.ReadHeaderTimeout != want.ReadHeaderTimeout || srv.ReadTimeout != want.ReadTimeout ||
		srv.WriteTimeout != want.WriteTimeout || srv.IdleTimeout != want.IdleTimeout ||
		srv.MaxHeaderBytes != want.MaxHeaderBytes {
		t.Fatalf("server limits %+v", srv)
	}
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http would print request details to stderr")
	}
}

// TestServerGuards: NewServer puts the guard in front of the handler.
func TestServerGuards(t *testing.T) {
	addr := startServer(t, NewServer(testSite, okServer().Handler, DefaultLimits), nil)
	out, err := rawGet(addr, 10*time.Second) // Host: x is not ours
	if err != nil || !strings.Contains(out, "421") {
		t.Fatalf("got %q %v, want 421", out, err)
	}
}

// TestSlowHeadersClosed: a client that never finishes its headers
// (slowloris) is cut off after ReadHeaderTimeout.
func TestSlowHeadersClosed(t *testing.T) {
	l := DefaultLimits
	l.ReadHeaderTimeout = 200 * time.Millisecond
	addr := startServer(t, NewServer(testSite, okServer().Handler, l), nil)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("GET / HTTP/1.1\r\nHost: " + testSite.Hosts[0] + "\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
	out, err := io.ReadAll(c)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("server kept a half-sent request open")
	}
	if strings.Contains(string(out), "200 OK") {
		t.Fatalf("served a half-sent request: %q", out)
	}
}

// TestBigHeaderRefused: headers over MaxHeaderBytes (plus net/http's fixed
// 4 KiB slack) get 431, and the handler never runs.
func TestBigHeaderRefused(t *testing.T) {
	addr := startServer(t, NewServer(testSite, okServer().Handler, DefaultLimits), nil)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	req := "GET / HTTP/1.1\r\nHost: " + testSite.Hosts[0] + "\r\nX-Big: " +
		strings.Repeat("a", 16<<10) + "\r\nConnection: close\r\n\r\n"
	_, _ = c.Write([]byte(req))
	out, _ := io.ReadAll(c)
	if !strings.Contains(string(out), "431") {
		t.Fatalf("got %q, want 431", out)
	}
}

// TestConnCapCountsCloseOnce:closing a connection twice frees one slot,
// not two.
func TestConnCapCountsCloseOnce(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lim := LimitConns(ln, 1).(*connLimiter)
	defer lim.Close()
	go func() {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			defer c.Close()
			_, _ = io.Copy(io.Discard, c)
		}
	}()
	c, err := lim.Accept()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close()
	if n := lim.open(); n != 0 {
		t.Fatalf("open = %d after a double close, want 0", n)
	}
}
