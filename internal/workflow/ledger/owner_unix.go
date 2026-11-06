//go:build unix

package ledger

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockOwner(path string) (*os.File, func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, nil, ErrOwned
		}
		return nil, nil, err
	}
	return f, nil, nil
}
