package dbclient

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ReachError is a database address that does not answer: nothing
// listens there, or it said nothing before the deadline. Via is the SSH
// login the address was asked through (user@host[:port]; "": a direct
// connect). The text is one plain sentence: the step that worked, the
// one that failed and how.
type ReachError struct {
	Addr       string // host:port of the database
	Via        string // the SSH login, user@host[:port] ("" = no tunnel)
	Timeout    bool   // it said nothing in time (else: refused)
	Prohibited bool   // the SSH server does not allow tunnels
	Reason     string // another reason from the SSH server
	// Hint is one more sentence of information (never an action termocode
	// takes), e.g. the container on that server that has the port.
	Hint string
	Err  error // the deadline, when Timeout came from one
}

func (e *ReachError) Error() string {
	msg := e.text()
	if h := sanitizeLine(e.Hint); h != "" {
		msg += " " + h
	}
	return msg
}

func (e *ReachError) text() string {
	addr, via := sanitizeLine(e.Addr), sanitizeLine(e.Via)
	if via != "" {
		head := "SSH to " + via + " works, but "
		switch {
		case e.Prohibited:
			return head + "that server does not allow tunnels (AllowTcpForwarding is off there)."
		case e.Timeout:
			return head + addr + " on that server did not answer in time (timed out)."
		case e.Reason != "":
			return head + addr + " on that server could not be reached (" + strings.TrimSuffix(sanitizeLine(e.Reason), ".") + ")."
		}
		return head + "nothing answers at " + addr + " on that server (connection refused)."
	}
	if e.Timeout {
		return "No answer from " + addr + " in time."
	}
	hint := "Is the database running there?"
	if host, _, err := net.SplitHostPort(e.Addr); err == nil && loopbackHost(host) {
		// The usual case: a database in a container whose port is not
		// published.
		hint = "Is the container's port published?"
	}
	return "Nothing answers at " + addr + ". " + hint
}

// Unwrap is the deadline of a timeout (errors.Is(err, DeadlineExceeded)).
func (e *ReachError) Unwrap() error { return e.Err }

// SSHLabel is user@host[:port] of an SSH login (no :22).
func SSHLabel(user, host string, port int) string {
	s := host
	if user != "" {
		s = user + "@" + s
	}
	if port != 0 && port != 22 {
		s += ":" + strconv.Itoa(port)
	}
	return sanitizeLine(s)
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
	for _, lead := range []string{"SSH to ", "Nothing answers at ", "No answer from "} {
		if i := strings.Index(msg, lead); i >= 0 && (lead != "SSH to " || strings.Contains(msg[i:], " works, but ")) {
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
