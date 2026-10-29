// Package netmsg says in plain words why a network request failed:
// refused, name not found, no answer in time, no route, or the proxy.
package netmsg

import (
	"errors"
	"net"
	"syscall"
)

// Why is the cause of a failed connection as a short sentence ("it
// refused the connection. Is the server running?"), and false when err
// is not a connection failure.
func Why(err error) (string, bool) {
	var op *net.OpError
	if !errors.As(err, &op) {
		return "", false
	}
	var dns *net.DNSError
	var ne net.Error
	switch {
	case op.Op == "proxyconnect":
		if errors.Is(op.Err, syscall.ECONNREFUSED) {
			return "the proxy " + proxyAddr(op) + " refused the connection.", true
		}
		return "the proxy " + proxyAddr(op) + " does not answer (" + op.Err.Error() + ").", true
	case errors.As(op.Err, &dns):
		return "name not found. Are you offline?", true
	case errors.Is(op.Err, syscall.ECONNREFUSED):
		return "it refused the connection. Is the server running?", true
	case errors.As(op.Err, &ne) && ne.Timeout():
		return "no answer in time. Are you offline?", true
	case errors.Is(op.Err, syscall.ENETUNREACH), errors.Is(op.Err, syscall.EHOSTUNREACH):
		return "no route to it. Are you offline?", true
	}
	return op.Err.Error(), true
}

// proxyAddr is the proxy's "host:port" of a proxyconnect error.
func proxyAddr(op *net.OpError) string {
	var inner *net.OpError
	if errors.As(op.Err, &inner) && inner.Addr != nil {
		return inner.Addr.String()
	}
	if op.Addr != nil {
		return op.Addr.String()
	}
	return "server"
}
