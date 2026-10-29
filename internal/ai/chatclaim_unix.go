//go:build unix

package ai

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockChat takes an exclusive, non-blocking flock on path.
func lockChat(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrChatInUse
		}
		return nil, err
	}
	return f, nil
}
