//go:build !unix

package dbclient

import (
	"os"
	"os/exec"
)

func setSQLiteProcessGroup(*exec.Cmd) {}
func interruptSQLiteProcess(p *sqliteProcess) {
	if p != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}
}
func killSQLiteProcess(p *sqliteProcess) {
	if p != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
