//go:build unix

package dbclient

import (
	"os/exec"
	"syscall"
)

func setSQLiteProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func interruptSQLiteProcess(p *sqliteProcess) {
	if p != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
	}
}

func killSQLiteProcess(p *sqliteProcess) {
	if p != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}
