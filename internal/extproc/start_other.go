//go:build !linux

package extproc

import (
	"errors"
	"os/exec"

	"github.com/amin-jalali/termocode/internal/crash"
)

// startAndWait starts cmd and returns a channel with cmd.Wait's result.
// Without Pdeathsig (Linux only) an extension notices that termocode is
// gone by the EOF on its stdin (see docs/extensions.md).
func startAndWait(cmd *exec.Cmd) (<-chan error, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	crash.GoWith("an extension process", func() { exited <- cmd.Wait() }, func() {
		select {
		case exited <- errors.New("an extension process failed"):
		default:
		}
	})
	return exited, nil
}
