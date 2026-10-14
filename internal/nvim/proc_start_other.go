//go:build !linux

package nvim

import "os/exec"

// startProc starts nvim. Without Pdeathsig (Linux only, proc_linux.go)
// the watchdog (watchdog.go) ends nvim when termocode dies.
func startProc(cmd *exec.Cmd, _ <-chan struct{}) error {
	return cmd.Start()
}
