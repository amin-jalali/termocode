//go:build unix

package webgw

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const testPort = 41237

var testSite = LoopbackSite(testPort)

// okHandler answers 200 "ok" and counts the requests that reached it.
type okHandler struct{ hits int }

func (h *okHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.hits++
	_, _ = w.Write([]byte("ok"))
}

func serveGuard(t *testing.T, r *http.Request) (*httptest.ResponseRecorder, *okHandler) {
	t.Helper()
	next := &okHandler{}
	w := httptest.NewRecorder()
	Guard(testSite, next).ServeHTTP(w, r)
	return w, next
}

// checkHeaders fails unless the response has every security header.
func checkHeaders(t *testing.T, h http.Header, what string) {
	t.Helper()
	for k, v := range wantHeaders(testSite) {
		if got := h.Get(k); got != v {
			t.Errorf("%s: %s = %q, want %q", what, k, got, v)
		}
	}
	for k := range h {
		if strings.HasPrefix(k, "Access-Control-") || k == "Set-Cookie" {
			t.Errorf("%s: forbidden header %s", what, k)
		}
	}
}

func TestHostAllowlist(t *testing.T) {
	p := strconv.Itoa(testPort)
	for _, tc := range []struct {
		host string
		ok   bool
	}{
		{"127.0.0.1:" + p, true},
		{"localhost:" + p, true},
		{"evil.com:" + p, false},
		{"evil.com", false},
		{"127.0.0.1:" + strconv.Itoa(testPort+1), false},
		{"127.0.0.1", false},
		{"localhost", false},
		{"", false},
		{"[::1]:" + p, false},
		{"LOCALHOST:" + p, false},
		{"localhost.:" + p, false},
		{"127.0.0.1:" + p + ".evil.com", false},
		{"0.0.0.0:" + p, false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = tc.host
		w, next := serveGuard(t, r)
		if tc.ok {
			if w.Code != http.StatusOK || next.hits != 1 {
				t.Errorf("Host %q: code %d hits %d, want 200 and a hit", tc.host, w.Code, next.hits)
			}
		} else if w.Code != http.StatusMisdirectedRequest || next.hits != 0 {
			t.Errorf("Host %q: code %d hits %d, want 421 and no hit", tc.host, w.Code, next.hits)
		}
		checkHeaders(t, w.Header(), "Host "+tc.host)
	}
}

// TestAbsoluteURIOtherHostRejected: "GET http://evil.com/ HTTP/1.1" with an
// allowed Host header still names another site.
func TestAbsoluteURIOtherHostRejected(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://evil.com/", nil)
	r.Host = testSite.Hosts[0]
	w, next := serveGuard(t, r)
	if w.Code != http.StatusMisdirectedRequest || next.hits != 0 {
		t.Fatalf("code %d hits %d, want 421 and no hit", w.Code, next.hits)
	}
}

// TestDNSRebindingRequestRejected: a page on evil.com whose name now points
// at 127.0.0.1 sends its own name as Host; over a real TCP connection it
// gets 421 and never reaches the handler.
func TestDNSRebindingRequestRejected(t *testing.T) {
	next := &okHandler{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: Guard(LoopbackSite(port), next)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req := "GET / HTTP/1.1\r\nHost: evil.com:" + strconv.Itoa(port) + "\r\nConnection: close\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status %d, want 421", resp.StatusCode)
	}
	if next.hits != 0 {
		t.Fatal("rebinding request reached the handler")
	}
}
