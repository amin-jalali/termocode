//go:build unix

package session

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// checkPeer refuses a connection from any uid but want. The socket mode
// already keeps other users out; this also covers a socket dir with loose
// permissions.
func checkPeer(c *net.UnixConn, want int) error {
	uid, err := peerUID(c)
	if err != nil {
		return fmt.Errorf("peer credentials: %w", err)
	}
	if uid != want {
		return fmt.Errorf("peer uid %d is not %d", uid, want)
	}
	return nil
}

// ownedByMe reports whether a file belongs to the current user.
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
