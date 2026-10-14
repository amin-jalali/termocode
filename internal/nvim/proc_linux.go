//go:build linux

package nvim

import (
	"os/exec"
	"runtime"
	"syscall"
)

// startProc starts nvim so that it gets SIGTERM when termocode dies,
// however it dies (Pdeathsig). nvim normally exits on EOF of its embed
// pipe, but not always (watchdog.go); the watchdog then finishes the job.
//
// Pdeathsig fires when the thread that started the child exits, not the
// process, so the start runs on a locked OS thread that lives until
// reaped is closed (nvim reaped). The goroutine then returns still
// locked, and the runtime ends that thread.
func startProc(cmd *exec.Cmd, reaped <-chan struct{}) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		err := cmd.Start()
		errc <- err
		if err != nil {
			runtime.UnlockOSThread()
			return
		}
		<-reaped
	}()
	return <-errc
}
