//go:build !unix

package clilogin

import (
	"errors"
	"os"
	"os/exec"
)

// The sign-in needs a pty; there is none on this system.
func startPty(*exec.Cmd, int, int) (*os.File, error) {
	return nil, errors.New("sign-in is not supported on this system")
}

func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
