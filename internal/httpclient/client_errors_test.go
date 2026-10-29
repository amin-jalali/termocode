package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// DNS problems on a checked send: an unknown host, a host with no
// address, and hosts whose every address refuses the connection.
func TestSendResolveFailures(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	resolver := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "empty.test":
			return nil, nil
		case "down.test":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	c := New(Options{Resolve: resolver})
	allow := Hooks{AllowIP: func(net.IP) bool { return true }}
	port := ":" + strconv.Itoa(closed)
	if r := send(t, c, "GET", "http://nope.test"+port+"/", "", allow); r.Err == "" || !strings.Contains(r.Err, "no such host") || r.Status != 0 {
		t.Errorf("unknown host: %v %q", r.ErrKind, r.Err)
	}
	if r := send(t, c, "GET", "http://empty.test"+port+"/", "", allow); !strings.Contains(r.Err, "lookup empty.test: no address") {
		t.Errorf("no address: %v %q", r.ErrKind, r.Err)
	}
	if r := send(t, c, "GET", "http://down.test"+port+"/", "", allow); r.ErrKind != ErrKindDial || !strings.Contains(r.Err, "refused") {
		t.Errorf("refused: %v %q", r.ErrKind, r.Err)
	}
}

// Without a resolver option the system resolver is used (localhost is
// answered from the hosts file, never the network).
func TestSendCheckedLocalhostUsesSystemResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var mu sync.Mutex
	var asked []string
	h := Hooks{AllowIP: func(ip net.IP) bool {
		mu.Lock()
		asked = append(asked, ip.String())
		mu.Unlock()
		return ip.IsLoopback()
	}}
	r := send(t, New(Options{}), "GET", "http://localhost:"+port+"/", "", h)
	mu.Lock()
	defer mu.Unlock()
	if string(r.Body) != "ok" || len(asked) == 0 {
		t.Fatalf("localhost: %q %q asked %v", r.Body, r.Err, asked)
	}
	if r.Timing.DNS <= 0 {
		t.Errorf("no DNS time for a name: %+v", r.Timing)
	}
}

// A checked send through a proxy dials the proxy, not the target: the
// target address is never asked about.
func TestSendCheckedThroughProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "via proxy "+r.Host) }))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	c := New(Options{Proxy: http.ProxyURL(pu), Resolve: func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("the target must not be resolved here")
	}})
	r := send(t, c, "GET", "http://api.example.test/x", "", Hooks{AllowIP: func(net.IP) bool { return false }})
	if string(r.Body) != "via proxy api.example.test" {
		t.Fatalf("%q %q", r.Body, r.Err)
	}
	p := func(*http.Request) (*url.URL, error) { return url.Parse("https://proxy.test") }
	if !isProxyAddr(p, "proxy.test:443") || isProxyAddr(p, "proxy.test:80") || isProxyAddr(nil, "x:1") {
		t.Error("isProxyAddr default ports")
	}
	p80 := func(*http.Request) (*url.URL, error) { return url.Parse("http://proxy.test") }
	if !isProxyAddr(p80, "proxy.test:80") {
		t.Error("http proxy without port is port 80")
	}
}

// A Host header sets the request's host; the server sees it.
func TestSendHostHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Host) }))
	defer srv.Close()
	r := send(t, New(Options{}), "GET", srv.URL, "", Hooks{}, Header{Name: "Host", Value: "tenant-a.example"})
	if string(r.Body) != "tenant-a.example" {
		t.Fatalf("%q %q", r.Body, r.Err)
	}
}

// rawServer answers every connection with reply (then closes).
func rawServer(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				_, _ = io.WriteString(c, reply)
			}()
		}
	}()
	return "http://" + ln.Addr().String()
}

// A status line without a reason phrase gets the standard one; a server
// that hangs up before answering is an error, not an empty 200.
func TestSendOddServers(t *testing.T) {
	u := rawServer(t, "HTTP/1.1 404\r\nContent-Length: 2\r\n\r\nno")
	r := send(t, New(Options{}), "GET", u, "", Hooks{})
	if r.Status != 404 || r.StatusText != "Not Found" || string(r.Body) != "no" {
		t.Errorf("no reason: %d %q %q %q", r.Status, r.StatusText, r.Body, r.Err)
	}
	u = rawServer(t, "")
	r = send(t, New(Options{}), "GET", u, "", Hooks{})
	if r.Err == "" || r.Status != 0 || r.ErrKind != ErrKindOther || !strings.Contains(r.Err, "EOF") {
		t.Errorf("hang up: %d %v %q", r.Status, r.ErrKind, r.Err)
	}
}

// A TLS server that only speaks an old protocol version: the message
// says what TLS refused.
func TestSendTLSVersionMismatch(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS11}
	srv.Config.ErrorLog = discardLog()
	srv.StartTLS()
	defer srv.Close()
	r := send(t, New(Options{}), "GET", srv.URL, "", Hooks{Insecure: true})
	if r.ErrKind != ErrKindTLS || !strings.HasPrefix(r.Err, "TLS: ") || strings.Contains(r.Err, "tls: ") {
		t.Fatalf("%v %q", r.ErrKind, r.Err)
	}
}

func TestTimeoutTexts(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Second: "2 s", 1500 * time.Millisecond: "1.5 s", 250 * time.Millisecond: "250 ms"} {
		if got := formatTimeout(d); got != want {
			t.Errorf("formatTimeout(%v) = %q, want %q", d, got, want)
		}
	}
	for in, want := range map[string]time.Duration{"5": 5 * time.Second, " 2s ": 2 * time.Second, "150ms": 150 * time.Millisecond} {
		if got, err := parseTimeout(in); err != nil || got != want {
			t.Errorf("parseTimeout(%q) = %v %v", in, got, err)
		}
	}
	for _, in := range []string{"soon", "5x", "05"} {
		if _, err := parseTimeout(in); err == nil {
			t.Errorf("parseTimeout(%q) took it", in)
		}
	}
}

// The proxy settings read like curl: a proxy without a scheme is http,
// NO_PROXY=* turns proxies off, NO_PROXY entries may carry a port, and
// localhost never goes through a proxy.
func TestEnvProxyRules(t *testing.T) {
	t.Setenv("HTTP_PROXY", "proxy.corp:3128")
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "internal.test:8080, .svc")
	t.Setenv("no_proxy", "")
	p := envProxy()
	get := func(raw string) string {
		u, _ := p(&http.Request{URL: mustURL(raw)})
		if u == nil {
			return ""
		}
		return u.String()
	}
	if got := get("http://api.test/"); got != "http://proxy.corp:3128" {
		t.Errorf("no scheme = %q", got)
	}
	for _, raw := range []string{"http://internal.test/", "http://a.svc/", "http://localhost:8080/", "http://app.localhost/"} {
		if got := get(raw); got != "" {
			t.Errorf("%s went through %q", raw, got)
		}
	}
	t.Setenv("NO_PROXY", "*")
	p = envProxy()
	if got := get("http://api.test/"); got != "" {
		t.Errorf("NO_PROXY=* = %q", got)
	}
	t.Setenv("HTTP_PROXY", "http://[::1")
	t.Setenv("NO_PROXY", "")
	p = envProxy()
	if got := get("http://api.test/"); got != "" {
		t.Errorf("a broken proxy URL is no proxy: %q", got)
	}
	c := New(Options{})
	c.Close() // nothing sent yet: no transport to close
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if r := send(t, c, "GET", srv.URL, "", Hooks{}); r.Status != 200 {
		t.Fatalf("%d %q", r.Status, r.Err)
	}
	c.Close()
}

func discardLog() *log.Logger { return log.New(io.Discard, "", 0) }
