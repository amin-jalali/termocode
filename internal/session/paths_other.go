//go:build !unix

package session

import "errors"

// errNoSessions: a session needs unix sockets and flock (ADR 0007);
// a native Windows build runs without them (CMP-95).
var errNoSessions = errors.New("persistent sessions are not supported on this system (use WSL)")

// PathsFor has no session files here.
func PathsFor(string) (Paths, error) { return Paths{}, errNoSessions }
