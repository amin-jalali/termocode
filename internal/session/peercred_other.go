//go:build unix && !linux && !darwin

package session

import (
	"net"
	"os"
)

// peerUID has no portable peer-credential call here; the 0700 socket dir
// and 0600 socket already keep other users out.
func peerUID(*net.UnixConn) (int, error) { return os.Getuid(), nil }
