//go:build !unix

package main

import (
	"errors"

	"github.com/amin-jalali/termocode/internal/traybar"
)

// Persistent sessions need unix sockets, so there is nothing to list.
func barSessions() ([]traybar.Session, error) {
	return nil, errors.New("persistent sessions are not supported on this system")
}
