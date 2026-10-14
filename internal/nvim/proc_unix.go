//go:build unix

package nvim

import (
	"os/exec"
	"syscall"
)

// detachTTY starts nvim in a new session, away from the terminal. A
// terminal hangup (a dropped SSH connection) sends SIGHUP to the
// foreground process group; nvim must not be in it, or it dies at once
// ("caught deadly signal", no autocmd runs) before termocode can flush
// the hot-exit backup. termocode gets the SIGHUP, flushes, and closes
// nvim. If termocode dies first, nvim reads EOF on its embed pipe and
// exits on its own; when it does not, Pdeathsig (proc_linux.go) and the
// watchdog (watchdog.go) end it.
func detachTTY(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
