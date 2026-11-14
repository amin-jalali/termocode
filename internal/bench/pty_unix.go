//go:build unix

package bench

import "syscall"

// killGroup kills the program and its process group.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
