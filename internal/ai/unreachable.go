package ai

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// unreachablePrefix starts every "could not reach the AI server" error.
const unreachablePrefix = "No connection to the AI server"

// IsUnreachable reports whether an error text says the AI server could
// not be reached (no network, refused, name not found, no answer).
func IsUnreachable(text string) bool { return strings.HasPrefix(text, unreachablePrefix) }

// requestError is the error of a failed HTTP request to the AI server:
// a network failure in plain words with the host, anything else (a stop
// by the user, a bad URL) as it was. The text is redacted.
func requestError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errString(Redact(err.Error()))
	}
	var ue *url.Error
	var op *net.OpError
	if !errors.As(err, &ue) || !errors.As(err, &op) {
		return errString(Redact(err.Error()))
	}
	host := ue.URL
	if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
		host = u.Host
	}
	var dns *net.DNSError
	var ne net.Error
	why := op.Err.Error()
	switch {
	case op.Op == "proxyconnect":
		why = "the proxy " + proxyAddr(op) + " refused the connection."
		if !errors.Is(op.Err, syscall.ECONNREFUSED) {
			why = "the proxy " + proxyAddr(op) + " does not answer (" + op.Err.Error() + ")."
		}
	case errors.As(op.Err, &dns):
		why = "name not found. Are you offline?"
	case errors.Is(op.Err, syscall.ECONNREFUSED):
		why = "it refused the connection. Is the server running?"
	case errors.As(op.Err, &ne) && ne.Timeout():
		why = "no answer in time. Are you offline?"
	case errors.Is(op.Err, syscall.ENETUNREACH), errors.Is(op.Err, syscall.EHOSTUNREACH):
		why = "no route to it. Are you offline?"
	}
	return errString(Redact(unreachablePrefix + " (" + host + "): " + why))
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
