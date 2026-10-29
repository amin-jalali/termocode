//go:build unix

package extproc

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the extension in its own process group, so a stop
// kills it AND every child it started, and a Ctrl+C in the terminal
// (sent to termocode's foreground group) does not reach it.
func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killGroup kills the extension's whole process group. It is safe after
// the extension itself exited: children it left behind are still in it.
func killGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
