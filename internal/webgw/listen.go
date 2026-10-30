//go:build unix

package webgw

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// ErrNotLoopback: the address would reach beyond this machine.
var ErrNotLoopback = errors.New("the web gateway listens on 127.0.0.1 only")

// ErrPortBusy: another program already listens on the port.
var ErrPortBusy = errors.New("the port is busy")

// loopbackHost is the one address v1 binds. Not "localhost" (a name may
// resolve elsewhere) and not ::1 (the Host allow list names 127.0.0.1).
const loopbackHost = "127.0.0.1"

// ListenConfig says where the gateway listens.
type ListenConfig struct {
	// Host must be "" or "127.0.0.1".
	Host string
	// Port is 0 for a random free port, else 1024..65535.
	Port int
}

// Listen opens the gateway's listener and returns the site a browser
// reaches it as. It refuses every address but 127.0.0.1: never a
// wildcard, a LAN address or a name (T15).
func Listen(cfg ListenConfig) (net.Listener, Site, error) {
	if cfg.Host != "" && cfg.Host != loopbackHost {
		return nil, Site{}, ErrNotLoopback
	}
	if cfg.Port != 0 && (cfg.Port < 1024 || cfg.Port > 65535) {
		return nil, Site{}, fmt.Errorf("web port %d: use 0 or 1024..65535", cfg.Port)
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort(loopbackHost, strconv.Itoa(cfg.Port)))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, Site{}, fmt.Errorf("web port %d: %w", cfg.Port, ErrPortBusy)
		}
		return nil, Site{}, err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		_ = ln.Close()
		return nil, Site{}, ErrNotLoopback
	}
	return ln, LoopbackSite(addr.Port), nil
}
