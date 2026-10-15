//go:build !linux

package extproc

import "os/exec"

// startAndWait starts cmd and returns a channel with cmd.Wait's result.
// Without Pdeathsig (Linux only) an extension notices that termocode is
// gone by the EOF on its stdin (see docs/extensions.md).
func startAndWait(cmd *exec.Cmd) (<-chan error, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}
