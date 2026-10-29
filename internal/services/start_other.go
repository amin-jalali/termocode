//go:build !linux

package services

import (
	"errors"
	"os/exec"

	"github.com/amin-jalali/termocode/internal/crash"
)

// startAndWait starts cmd and returns a channel with cmd.Wait's result.
func startAndWait(cmd *exec.Cmd) (<-chan error, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	crash.GoWith("a service process", func() { exited <- cmd.Wait() }, func() {
		select {
		case exited <- errors.New("a service process failed"):
		default:
		}
	})
	return exited, nil
}
