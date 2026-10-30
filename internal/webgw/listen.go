//go:build unix

package webgw

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
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
	// Tailnet lets tailscaled (root) connect too: Tailscale Serve proxies
	// from it (WEB-31). The address is still 127.0.0.1 only.
	Tailnet bool
}

// Listen opens the gateway's listener and returns the site a browser
// reaches it as. It refuses every address but 127.0.0.1: never a
// wildcard, a LAN address or a name (T15). On Linux every accepted
// connection's peer must run as our uid (checkTCPPeer); another user's
// is closed before anything is read (T16).
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
	return &peerListener{Listener: ln, check: peerCheck(cfg.Tailnet)}, LoopbackSite(addr.Port), nil
}

// ErrSocketBusy: another process answers on the socket path.
var ErrSocketBusy = errors.New("another web gateway already serves this folder")

// ListenUnix listens on a unix socket for SSH mode: the user's own ssh
// forwards 127.0.0.1:portHint on their computer to path, so the far host
// opens no TCP port at all. The socket is 0600 in a folder that must
// already be private (0700, ours), and every connection's peer must run as
// our uid (session.CheckPeer) before a byte is read (T14). A socket file
// nobody answers on is removed first; one that answers is ErrSocketBusy;
// any other kind of file is left alone and is an error.
func ListenUnix(path string, portHint int) (net.Listener, Site, error) {
	return listenUnix(path, portHint, session.CheckPeer)
}

func listenUnix(path string, portHint int, check func(net.Conn) error) (net.Listener, Site, error) {
	if portHint < 1024 || portHint > 65535 {
		return nil, Site{}, fmt.Errorf("web port hint %d: use 1024..65535", portHint)
	}
	if err := privateDir(filepath.Dir(path)); err != nil {
		return nil, Site{}, err
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, Site{}, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = c.Close()
			return nil, Site{}, ErrSocketBusy
		}
		if err := os.Remove(path); err != nil {
			return nil, Site{}, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, Site{}, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, Site{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, Site{}, err
	}
	return &peerListener{Listener: ln, check: check}, LoopbackSite(portHint), nil
}

// privateDir refuses a folder other users can enter or one we do not own.
func privateDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s must be a private folder (0700) owned by you", dir)
	}
	return nil
}

// peerListener hands out only connections whose peer passes check. A
// refused one is closed at once, before anything is read from it, and
// Accept goes on waiting.
type peerListener struct {
	net.Listener
	check func(net.Conn) error
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.check == nil || l.check(c) != nil {
			_ = c.Close()
			continue
		}
		return c, nil
	}
}
