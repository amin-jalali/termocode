//go:build unix

package webgw

import (
	"errors"
	"net"
	"strconv"
	"testing"
)

func TestListenLoopbackRandomPort(t *testing.T) {
	ln, site, err := Listen(ListenConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) || addr.Port == 0 {
		t.Fatalf("listening on %v", addr)
	}
	p := strconv.Itoa(addr.Port)
	if site.Scheme != "http" || len(site.Hosts) != 2 ||
		site.Hosts[0] != "127.0.0.1:"+p || site.Hosts[1] != "localhost:"+p {
		t.Fatalf("site %+v for port %s", site, p)
	}
}

// TestRefusesWildcard: 0.0.0.0 and :: would serve every interface (T15).
func TestRefusesWildcard(t *testing.T) {
	for _, h := range []string{"0.0.0.0", "::", "[::]", "0", "0.0.0.0:0"} {
		ln, _, err := Listen(ListenConfig{Host: h})
		if err == nil {
			ln.Close()
			t.Errorf("Listen on %q: no error", h)
		} else if !errors.Is(err, ErrNotLoopback) {
			t.Errorf("Listen on %q: %v, want ErrNotLoopback", h, err)
		}
	}
}

// TestRefusesLANAddress: only 127.0.0.1 itself. A LAN or public address,
// another loopback address the Host check does not know, IPv6 loopback
// and names (which may resolve anywhere) are refused.
func TestRefusesLANAddress(t *testing.T) {
	hosts := []string{"192.168.1.10", "10.0.0.5", "172.16.0.1", "8.8.8.8",
		"127.0.0.2", "::1", "localhost", "example.com", "fe80::1"}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				hosts = append(hosts, ipn.IP.String())
			}
		}
	}
	for _, h := range hosts {
		ln, _, err := Listen(ListenConfig{Host: h})
		if err == nil {
			ln.Close()
			t.Errorf("Listen on %q: no error", h)
		} else if !errors.Is(err, ErrNotLoopback) {
			t.Errorf("Listen on %q: %v, want ErrNotLoopback", h, err)
		}
	}
}

func TestExplicitLoopbackHostAccepted(t *testing.T) {
	ln, _, err := Listen(ListenConfig{Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestPortRange(t *testing.T) {
	for _, p := range []int{-1, 1, 80, 1023, 65536} {
		ln, _, err := Listen(ListenConfig{Port: p})
		if err == nil {
			ln.Close()
			t.Errorf("port %d: no error", p)
		}
	}
}

func TestPortBusyError(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	if port < 1024 {
		t.Skipf("kernel gave a low port %d", port)
	}
	ln, _, err := Listen(ListenConfig{Port: port})
	if err == nil {
		ln.Close()
		t.Fatal("no error on a busy port")
	}
	if !errors.Is(err, ErrPortBusy) {
		t.Fatalf("err %v, want ErrPortBusy", err)
	}
}
