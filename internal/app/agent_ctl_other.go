//go:build !unix

package app

import "net"

// No control socket off unix (the session folder is unix only).
func agentCtlSocketPath(string) string { return "" }

var agentCtlPeer = func(net.Conn) error { return nil }
