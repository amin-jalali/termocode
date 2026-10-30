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

// TestConnCapCountsCloseOnce: closing a connection twice frees one slot,
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
