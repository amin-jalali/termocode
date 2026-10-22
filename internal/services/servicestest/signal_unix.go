//go:build unix

package servicestest

import "syscall"

// signalZero reports whether pid exists (kill -0).
func signalZero(pid int) error { return syscall.Kill(pid, 0) }
