//go:build unix

package app

import (
	"encoding/json"
	"os"
	"syscall"

	"github.com/amin-jalali/termocode/internal/session"
)

// webDoorAttached reports whether a terminal is attached to this UI's
// session (its info file says so): the user has it on the laptop now.
// Tests swap it.
var webDoorAttached = func() bool {
	p, err := session.PathsFor(aiWorkspaceRoot())
	if err != nil {
		return false
	}
	b, err := os.ReadFile(p.Info)
	if err != nil {
		return false
	}
	var in struct {
		Attached bool `json:"attached"`
	}
	return json.Unmarshal(b, &in) == nil && in.Attached
}

// webDoorQuit ends this UI the way `termocode kill` does: SIGTERM, so
// unsaved buffers go to the hot-exit backup first (cmd/termocode's
// watchHangup). Tests swap it.
var webDoorQuit = func() { _ = syscall.Kill(os.Getpid(), syscall.SIGTERM) }
