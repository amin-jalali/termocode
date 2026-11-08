package dbclient

import (
	"errors"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ReachError is a database address that does not answer: nothing
// listens there, or it said nothing before the deadline. Via is the SSH
// server the address was asked from ("": a direct connect). The text is
// one plain line that says what to check.
type ReachError struct {
	Addr       string // host:port of the database
	Via        string // the SSH server ("" = no tunnel)
	Timeout    bool   // it said nothing in time (else: refused)
	Prohibited bool   // the SSH server does not allow tunnels
	Reason     string // another reason from the SSH server
}

func (e *ReachError) Error() string {
	addr, via := sanitizeLine(e.Addr), sanitizeLine(e.Via)
	switch {
	case e.Prohibited:
		return via + " does not allow SSH tunnels (AllowTcpForwarding is off there)."
	case e.Timeout && via != "":
		return "No answer from " + addr + " on " + via + " (through SSH) in time."
	case e.Timeout:
		return "No answer from " + addr + " in time."
	case e.Reason != "":
		return via + " could not reach " + addr + " (through SSH): " + sanitizeLine(e.Reason)
	}
	hint := "Is the database running there?"
	if host, _, err := net.SplitHostPort(e.Addr); err == nil && loopbackHost(host) {
		// The usual case: a database in a container whose port is not
		// published on the server.
		hint = "Is the container's port published?"
	}
	if via == "" {
		return "Nothing answers at " + addr + ". " + hint
	}
	return "Nothing answers at " + addr + " on " + via + " (through SSH). " + hint
}

// ReachText is the ReachError line inside err's text (a driver that
// flattens its errors keeps the words), or "".
func ReachText(err error) string {
	if err == nil {
		return ""
	}
	var reach *ReachError
	if errors.As(err, &reach) {
		return reach.Error()
	}
	msg := err.Error()
	for _, lead := range []string{"Nothing answers at ", "No answer from "} {
		if i := strings.Index(msg, lead); i >= 0 {
			return sanitizeLine(msg[i:])
		}
	}
	return ""
}

func loopbackHost(h string) bool {
	switch strings.ToLower(strings.Trim(h, "[]")) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// tunnelReachError is the error of a tunnel channel to addr that the SSH
// server via refused.
func tunnelReachError(addr, via string, err error) error {
	e := &ReachError{Addr: addr, Via: via}
	var open *ssh.OpenChannelError
	if errors.As(err, &open) {
		switch open.Reason {
		case ssh.ConnectionFailed:
			return e
		case ssh.Prohibited:
			e.Prohibited = true
			return e
		}
	}
	e.Reason = strings.TrimPrefix(err.Error(), "ssh: ")
	return e
}
