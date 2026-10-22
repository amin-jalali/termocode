//go:build unix

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// maxSocketPath is a safe length for a unix socket path (sun_path is 104
// bytes on macOS, 108 on Linux, including the final NUL).
const maxSocketPath = 100

// Dir returns the folder that holds the session sockets, creating it with
// mode 0700: $XDG_RUNTIME_DIR/termocode, else
// $XDG_CACHE_HOME/termocode/sessions (~/.cache/termocode/sessions). A path
// too long for a socket falls back to $TMPDIR/termocode-<uid>.
func Dir() (string, error) {
	var dir string
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		dir = filepath.Join(v, "termocode")
	} else {
		cache := os.Getenv("XDG_CACHE_HOME")
		if cache == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			cache = filepath.Join(home, ".cache")
		}
		dir = filepath.Join(cache, "termocode", "sessions")
	}
	if len(dir)+len("/0123456789abcdef.mcp.sock") > maxSocketPath { // the longest socket name

		dir = filepath.Join(os.TempDir(), fmt.Sprintf("termocode-%d", os.Getuid()))
	}
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ensurePrivateDir creates dir (mode 0700) and refuses a dir that is a
// symlink, that another user owns or that others can open: the sockets
// inside give full control of the editor, and the $TMPDIR fallback is
// shared with other users, who could create it first.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err == nil {
		// Ours and new: undo a umask that took the owner bits.
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("session dir %s is a symlink", dir)
	case !fi.IsDir():
		return fmt.Errorf("session dir %s is not a folder", dir)
	case !ownedByMe(fi):
		return fmt.Errorf("session dir %s is not owned by this user", dir)
	case fi.Mode().Perm() != 0o700:
		return fmt.Errorf("session dir %s has mode %o, want 700", dir, fi.Mode().Perm())
	}
	return nil
}

// Paths are the files of one workspace session.
type Paths struct {
	Workspace string // absolute workspace folder
	Key       string
	Socket    string // unix socket the server listens on
	Lock      string // flock held by the live server
	Info      string // JSON with the workspace, pid and start time
	Log       string // server stderr
	Caps      string // caps of the last client that attached (ClientCaps)
	// MCPSocket is where the UI serves MCP to outside agents (M1); the
	// `termocode mcp` relay connects to it.
	MCPSocket string
}

// PathsFor returns the session files for a workspace folder.
func PathsFor(workspace string) (Paths, error) {
	dir, err := Dir()
	if err != nil {
		return Paths{}, err
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return Paths{}, err
	}
	k := Key(abs)
	base := filepath.Join(dir, k)
	return Paths{
		Workspace: filepath.Clean(abs),
		Key:       k,
		Socket:    base + ".sock",
		Lock:      base + ".lock",
		Info:      base + ".json",
		Log:       base + ".log",
		Caps:      base + ".caps.json",
		MCPSocket: base + ".mcp.sock",
	}, nil
}
