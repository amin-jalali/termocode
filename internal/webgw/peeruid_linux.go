//go:build linux

package webgw

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// The peer of a TCP connection on 127.0.0.1 can be any local user. On
// Linux the kernel lists every TCP socket with its owner's uid in
// /proc/net/tcp (and tcp6 for dual-stack sockets), so the gateway refuses
// a browser that runs as another user before a byte is read (T16). Other
// systems have no such table; there the launch secret and the token are
// the only lock.

// errPeerUID: the connection's peer runs as another user, or its owner
// could not be found (fail closed).
var errPeerUID = errors.New("the web connection comes from another user")

// procNetFiles are read in order; tests swap the opener.
var procNetFiles = []string{"/proc/net/tcp", "/proc/net/tcp6"}

var openProcNet = func(name string) (io.ReadCloser, error) { return os.Open(name) }

// checkTCPPeer accepts a TCP connection whose peer socket is owned by our
// uid. Anything but TCP passes (a unix socket has its own peer check).
func checkTCPPeer(c net.Conn) error { return checkPeerUID(c, false) }

// peerCheck is the peer check of a listener: checkTCPPeer, and in
// tailnet mode also root, the owner of tailscaled, whose Serve proxy is
// the only way a tailnet browser reaches the gateway.
func peerCheck(tailnet bool) func(net.Conn) error {
	return func(c net.Conn) error { return checkPeerUID(c, tailnet) }
}

func checkPeerUID(c net.Conn, allowRoot bool) error {
	local, ok1 := c.LocalAddr().(*net.TCPAddr)
	remote, ok2 := c.RemoteAddr().(*net.TCPAddr)
	if !ok1 || !ok2 {
		return nil
	}
	uid, err := peerUID(local.AddrPort(), remote.AddrPort())
	if err != nil {
		return fmt.Errorf("%w: %v", errPeerUID, err)
	}
	if uid != os.Getuid() && !(allowRoot && uid == 0) {
		return errPeerUID
	}
	return nil
}

// peerUID finds the owner of the peer's socket: the row whose local
// address is the peer and whose remote address is us.
func peerUID(us, peer netip.AddrPort) (int, error) {
	for _, name := range procNetFiles {
		f, err := openProcNet(name)
		if err != nil {
			continue
		}
		uid, found, err := findProcNetUID(f, peer, us)
		_ = f.Close()
		if err != nil {
			return 0, err
		}
		if found {
			return uid, nil
		}
	}
	return 0, errors.New("peer socket not found")
}

// findProcNetUID scans a /proc/net/tcp{,6} table for the socket with this
// local and remote address and returns its uid. IPv4-mapped IPv6 rows
// match their IPv4 address.
func findProcNetUID(r io.Reader, local, remote netip.AddrPort) (int, bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	first := true
	for sc.Scan() {
		if first { // the header row
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 8 {
			continue
		}
		l, err1 := parseProcAddr(f[1])
		rm, err2 := parseProcAddr(f[2])
		if err1 != nil || err2 != nil || l != unmap(local) || rm != unmap(remote) {
			continue
		}
		uid, err := strconv.ParseUint(f[7], 10, 32)
		if err != nil {
			return 0, false, fmt.Errorf("bad uid column %q", f[7])
		}
		return int(uid), true, nil
	}
	return 0, false, sc.Err()
}

func unmap(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// parseProcAddr reads "0100007F:1F90" (IPv4) or a 32-digit IPv6 address
// with its port. The kernel prints each 32-bit word of the address in
// host byte order and the port as a plain number.
func parseProcAddr(s string) (netip.AddrPort, error) {
	hexAddr, hexPort, ok := strings.Cut(s, ":")
	if !ok || len(hexPort) != 4 || (len(hexAddr) != 8 && len(hexAddr) != 32) {
		return netip.AddrPort{}, fmt.Errorf("bad address %q", s)
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("bad port %q", s)
	}
	raw, err := hex.DecodeString(hexAddr)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("bad address %q", s)
	}
	b := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(b[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	addr, _ := netip.AddrFromSlice(b)
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
}
