//go:build linux

package nvim

import (
	"os/exec"
	"testing"
)

// startProc gives a command a parent-death signal even when it had no
// process attributes, and reports a failed start. (Linux only: Pdeathsig
// exists there; moved here from cov_test.go so darwin and windows build.)
func TestStartProcSetsAttrsAndReportsFailure(t *testing.T) {
	reaped := make(chan struct{})
	cmd := exec.Command("true")
	if err := startProc(cmd, reaped); err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig == 0 {
		t.Fatal("no parent-death signal")
	}
	_ = cmd.Wait()
	close(reaped)
	if err := startProc(exec.Command("/no/such/binary"), make(chan struct{})); err == nil {
		t.Fatal("a missing binary started")
	}
}
