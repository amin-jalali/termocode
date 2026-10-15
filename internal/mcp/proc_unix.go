//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
)

// Same helpers as internal/extproc/proc_unix.go: the server runs in its
// own process group, so a stop kills it AND every child it started (npx
// → node), and a Ctrl+C in the terminal does not reach it.

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

func termGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
}
