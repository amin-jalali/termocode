//go:build !unix

package app

import "os/exec"

func setTestProcGroup(*exec.Cmd) {}

func killTestProc(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
