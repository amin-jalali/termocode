//go:build unix

package git

import (
	"os/exec"
	"syscall"
)

// detachTTY starts cmd in a new session: it has no controlling terminal,
// so nothing it runs can open /dev/tty.
func detachTTY(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
