//go:build linux

package services

import (
	"errors"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/amin-jalali/termocode/internal/crash"
)

// startAndWait starts cmd so that it gets SIGTERM when termocode dies
// (Pdeathsig) and returns a channel with cmd.Wait's result. Pdeathsig
// fires when the thread that started the child exits, so the start and
// the wait run on one locked OS thread (as in internal/extproc).
func startAndWait(cmd *exec.Cmd) (<-chan error, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	started := make(chan error, 1)
	exited := make(chan error, 1)
	crash.GoWith("a service start", func() {
		runtime.LockOSThread()
		if err := cmd.Start(); err != nil {
			runtime.UnlockOSThread()
			started <- err
			return
		}
		started <- nil
		exited <- cmd.Wait()
		// Return still locked: the runtime ends this thread.
	}, func() {
		failed := errors.New("a service process failed")
		select {
		case started <- failed:
		default:
		}
		select {
		case exited <- failed:
		default:
		}
	})
	if err := <-started; err != nil {
		return nil, err
	}
	return exited, nil
}
