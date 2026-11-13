//go:build unix

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// maxSocketPath is a safe length for a unix socket path (sun_path is 104
// bytes on macOS, 108 on Linux, including the final NUL).
const maxSocketPath = 100

// longestSocketName is the longest socket file name in the session dir
// (the web gateway's HTTP socket); Dir keeps it inside sun_path.
const longestSocketName = "/0123456789abcdef.webhttp.sock"

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
	if len(dir)+len(longestSocketName) > maxSocketPath {
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
	return pathsAt(dir, Key(abs), filepath.Clean(abs)), nil
}

// PathsForID returns the session files of a workspace ID (RC-5): a
// local ID is PathsFor its folder; a container ID is keyed on the whole
// ID, so its socket never meets a local folder's or another container's
// with the same path. Paths.Workspace is then the ID.
func PathsForID(id string) (Paths, error) {
	kind, rest := workspace.ParseID(id)
	switch kind {
	case workspace.KindLocal:
		return PathsFor(rest)
	case workspace.KindContainer:
		if rest == "" {
			break
		}
		dir, err := Dir()
		if err != nil {
			return Paths{}, err
		}
		return pathsAt(dir, KeyForID(id), id), nil
	}
	return Paths{}, fmt.Errorf("not a workspace ID: %q (want local:/folder or container:<context>/<name>:/folder)", id)
}

// pathsAt is the session files of key k in dir.
func pathsAt(dir, k, ws string) Paths {
	base := filepath.Join(dir, k)
	return Paths{
		Workspace:   ws,
		Key:         k,
		Socket:      base + ".sock",
		Lock:        base + ".lock",
		Info:        base + ".json",
		Log:         base + ".log",
		Caps:        base + ".caps.json",
		MCPSocket:   base + ".mcp.sock",
		AgentSocket: base + ".agent.sock",

		WebSocket:     base + ".web.sock",
		WebHTTPSocket: base + ".webhttp.sock",
		WebStatus:     base + ".web.json",
		WebLock:       base + ".web.lock",
		WebResume:     base + ".web-resume.json",
		WebOpen:       base + ".web-open.html",
	}
}
