//go:build unix

package clistep

import (
	"os/exec"
	"syscall"
)

// The CLI runs in its own process group: a Stop or a timeout kills it
// and everything it started (node, shells, tests).

func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
