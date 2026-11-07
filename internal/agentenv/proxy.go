package agentenv

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Proxy is a local HTTP proxy that keeps a Policy: CONNECT tunnels
// (https) and plain http requests to an allowed host pass, the rest get
// 403 and are recorded. The agent's tools find it through HTTP(S)_PROXY;
// a tool that ignores those is not held (this is not an OS sandbox).
type Proxy struct {
	policy Policy
	ln     net.Listener
	srv    *http.Server
	tr     *http.Transport
	dialer net.Dialer

	mu      sync.Mutex
	blocked map[string]bool
	tunnels map[net.Conn]bool
	closed  bool
}

// StartProxy listens on a free 127.0.0.1 port.
func StartProxy(p Policy) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("network rule: the local proxy did not start: %w", err)
	}
	x := &Proxy{policy: p, ln: ln, blocked: map[string]bool{}, tunnels: map[net.Conn]bool{}, dialer: net.Dialer{Timeout: 30 * time.Second}}
	x.tr = &http.Transport{Proxy: nil, DialContext: x.dialer.DialContext, ResponseHeaderTimeout: 2 * time.Minute, MaxIdleConns: 16}
	x.srv = &http.Server{Handler: x, ReadHeaderTimeout: 30 * time.Second}
	crash.Go("agent network proxy", func() { _ = x.srv.Serve(ln) })
	return x, nil
}

// Addr is host:port of the proxy.
func (x *Proxy) Addr() string { return x.ln.Addr().String() }

// EnvVars point the usual proxy variables at the proxy (both cases).
func (x *Proxy) EnvVars() []string {
	u := "http://" + x.Addr()
	no := "localhost,127.0.0.1,::1"
	return []string{
		"HTTP_PROXY=" + u, "HTTPS_PROXY=" + u, "ALL_PROXY=" + u, "NO_PROXY=" + no,
		"http_proxy=" + u, "https_proxy=" + u, "all_proxy=" + u, "no_proxy=" + no,
	}
}

// Blocked is the hosts that were refused, sorted.
func (x *Proxy) Blocked() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := make([]string, 0, len(x.blocked))
	for h := range x.blocked {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Close stops the proxy and its open tunnels.
func (x *Proxy) Close() error {
	x.mu.Lock()
	x.closed = true
	for c := range x.tunnels {
		c.Close()
	}
	x.tunnels = map[net.Conn]bool{}
	x.mu.Unlock()
	x.tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := x.srv.Shutdown(ctx); err != nil {
		return x.srv.Close()
	}
	return nil
}

func (x *Proxy) block(w http.ResponseWriter, host string) {
	h := normHost(host)
	x.mu.Lock()
	if len(x.blocked) < 1000 {
		x.blocked[h] = true
	}
	x.mu.Unlock()
	http.Error(w, "termocode: the network rule in "+File+" blocks "+h, http.StatusForbidden)
}

// hop are the hop-by-hop headers a proxy drops.
var hop = []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

// ServeHTTP handles one proxy request.
func (x *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		x.connect(w, r)
		return
	}
	if !r.URL.IsAbs() || r.URL.Host == "" {
		http.Error(w, "termocode: this is a proxy", http.StatusBadRequest)
		return
	}
	if !x.policy.Allows(r.URL.Host) {
		x.block(w, r.URL.Host)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hop {
		out.Header.Del(h)
	}
	resp, err := x.tr.RoundTrip(out)
	if err != nil {
		http.Error(w, "termocode proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range hop {
		resp.Header.Del(h)
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (x *Proxy) connect(w http.ResponseWriter, r *http.Request) {
	if !x.policy.Allows(r.Host) {
		x.block(w, r.Host)
		return
	}
	addr := r.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	up, err := x.dialer.DialContext(r.Context(), "tcp", addr)
	if err != nil {
		http.Error(w, "termocode proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "termocode proxy: no tunnel", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	if _, err := down.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		up.Close()
		down.Close()
		return
	}
	if !x.track(up, down) {
		up.Close()
		down.Close()
		return
	}
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			up.Close()
			down.Close()
			x.mu.Lock()
			delete(x.tunnels, up)
			delete(x.tunnels, down)
			x.mu.Unlock()
		})
	}
	crash.Go("agent network tunnel", func() {
		defer closeBoth()
		if n := buf.Reader.Buffered(); n > 0 {
			b, _ := buf.Reader.Peek(n)
			if _, err := up.Write(b); err != nil {
				return
			}
		}
		_, _ = io.Copy(up, down)
	})
	crash.Go("agent network tunnel", func() {
		defer closeBoth()
		_, _ = io.Copy(down, up)
	})
}

// track adds a tunnel's two ends; false when the proxy is closed.
func (x *Proxy) track(cs ...net.Conn) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed {
		return false
	}
	for _, c := range cs {
		x.tunnels[c] = true
	}
	return true
}
