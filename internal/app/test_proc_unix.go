//go:build unix

package app

import (
	"os/exec"
	"syscall"
)

// setTestProcGroup puts the test command in its own process group so a
// stop kills the runner AND the test binaries it spawned (go test, cargo).
func setTestProcGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTestProc kills the whole process group of c.
func killTestProc(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
		_ = c.Process.Kill()
	}
}
