//go:build unix

package mcp

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/amin-jalali/termocode/internal/session"
)

// maxShareSocket is a safe unix socket path length (sun_path is 104
// bytes on macOS, 108 on Linux).
const maxShareSocket = 100

// shareDir returns the private socket folder (0700, ours, no symlink),
// creating it.
func shareDir(o *ShareOptions) (string, error) {
	dir := o.Dir
	if dir == "" {
		base, err := session.Dir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "mcp")
	}
	if len(dir)+len("/")+32+len(".sock") > maxShareSocket {
		return "", fmt.Errorf("socket folder %s is too long for a unix socket", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err == nil {
		if err := os.Chmod(dir, 0o700); err != nil { // undo the umask
			return "", err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return "", fmt.Errorf("%s is a symlink", dir)
	case !fi.IsDir():
		return "", fmt.Errorf("%s is not a folder", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return "", fmt.Errorf("%s is not owned by this user", dir)
	case fi.Mode().Perm() != 0o700:
		return "", fmt.Errorf("%s has mode %o, want 700", dir, fi.Mode().Perm())
	}
	return dir, nil
}

// shareSalt is the per-user HMAC key of shareKey: 32 random bytes in
// <dir>/salt (0600), made by the first session.
func shareSalt(dir string) ([]byte, error) {
	p := filepath.Join(dir, "salt")
	for range 3 {
		b, err := os.ReadFile(p)
		if err == nil {
			if len(b) != 32 {
				return nil, fmt.Errorf("%s: bad salt", p)
			}
			return b, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(p+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) { // another session writes it now
				_ = os.Remove(p + ".tmp")
				continue
			}
			return nil, err
		}
		_, werr := f.Write(salt)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(p + ".tmp")
			return nil, errors.Join(werr, cerr)
		}
		// Link fails when another session made the salt first: use theirs.
		lerr := os.Link(p+".tmp", p)
		_ = os.Remove(p + ".tmp")
		if lerr != nil && !errors.Is(lerr, os.ErrExist) {
			return nil, lerr
		}
	}
	return nil, errors.New("could not make the share salt")
}
