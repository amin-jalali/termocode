//go:build unix && !linux

package webgw

import "net"

// checkTCPPeer has no table of socket owners to read on this system; the
// launch secret and the token are the lock (see peeruid_linux.go).
func checkTCPPeer(net.Conn) error { return nil }
