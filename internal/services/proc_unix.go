//go:build unix

package services

import (
	"os/exec"
	"syscall"
)

// Same helpers as internal/mcp/proc_unix.go: docker runs in its own
// process group, so a cancel kills `docker compose` and the plugin it
// started, and a Ctrl+C in the terminal does not reach it.

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
