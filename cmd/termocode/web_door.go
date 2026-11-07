//go:build unix

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// errNoProject: the phone named a workspace that does not run now (or
// is not a workspace key at all).
var errNoProject = errors.New("that project is not running")

// webDoorDial opens the web hub of another running workspace of this
// user by its key (MOB-50, the door). The key is 16 hex digits, never a
// path, so the phone cannot name a folder; the socket must be a real
// unix socket in this user's session folder (no symlink), and the hub on
// the other end checks the peer is the same user (ADR 0020).
func webDoorDial(ctx context.Context, home, ws string) (net.Conn, error) {
	if !webproto.ValidWS(ws) || ws == home {
		return nil, errNoProject
	}
	dir, err := session.Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ws+".web.sock")
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode().Type() != os.ModeSocket {
		return nil, errNoProject
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errNoProject
	}
	return c, nil
}
