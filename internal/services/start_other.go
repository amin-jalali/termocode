//go:build !linux

package services

import "os/exec"

// startAndWait starts cmd and returns a channel with cmd.Wait's result.
func startAndWait(cmd *exec.Cmd) (<-chan error, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}
