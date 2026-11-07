package agentenv

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// proxyClient sends plain http through the proxy.
func proxyClient(px *Proxy) *http.Client {
	u, _ := url.Parse("http://" + px.Addr())
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
}

func TestProxyForwardsAnAllowedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	defer srv.Close()
	px, err := StartProxy(Network{Mode: NetOpen}.Policy())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	resp, err := proxyClient(px).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hello" || len(px.Blocked()) != 0 {
		t.Errorf("body %q blocked %v", b, px.Blocked())
	}
}

func TestProxyBlocksAndRecords(t *testing.T) {
	px, err := StartProxy(Network{Mode: NetAllow, Allow: []string{"allowed.example"}}.Policy())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	resp, err := proxyClient(px).Get("http://blocked.example/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", resp.StatusCode)
	}

	// CONNECT (https) to a blocked host.
	c, err := net.Dial("tcp", px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "CONNECT other.example:443 HTTP/1.1\r\nHost: other.example:443\r\n\r\n")
	r, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != http.StatusForbidden {
		t.Errorf("connect status %d", r.StatusCode)
	}
	got := strings.Join(px.Blocked(), ",")
	if got != "blocked.example,other.example" {
		t.Errorf("blocked %q", got)
	}
}

func TestProxyTunnelsAnAllowedConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(c, "pong")
	}()
	px, err := StartProxy(Network{Mode: NetOpen}.Policy())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	c, err := net.Dial("tcp", px.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", ln.Addr(), ln.Addr())
	br := bufio.NewReader(c)
	r, err := http.ReadResponse(br, nil)
	if err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("connect: %v %v", r, err)
	}
	b, _ := io.ReadAll(br)
	if string(b) != "pong" {
		t.Errorf("tunnel %q", b)
	}
}

func TestProxyEnvVars(t *testing.T) {
	px, err := StartProxy(Network{Mode: NetOff}.Policy())
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	env := strings.Join(px.EnvVars(), "\n")
	for _, k := range []string{"HTTP_PROXY=", "HTTPS_PROXY=", "http_proxy=", "https_proxy=", "ALL_PROXY=", "NO_PROXY="} {
		if !strings.Contains(env, k) {
			t.Errorf("no %s in %s", k, env)
		}
	}
	if !strings.Contains(env, px.Addr()) {
		t.Errorf("no address in %s", env)
	}
}
