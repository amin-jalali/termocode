//go:build !unix

package extproc

import (
	"os"
	"os/exec"
)

func setProcGroup(*exec.Cmd) {}

func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
