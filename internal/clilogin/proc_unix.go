//go:build unix

package clilogin

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// startPty starts cmd on a new pty of cols×rows. The pty makes the CLI
// a session (and process group) leader, so killGroup reaches everything
// it started.
func startPty(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func killGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
