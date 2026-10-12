//go:build !unix

package nvim

import "os/exec"

// detachTTY is a no-op where there are no sessions (see proc_unix.go).
func detachTTY(*exec.Cmd) {}
