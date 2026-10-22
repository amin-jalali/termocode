//go:build !unix

package servicestest

import "errors"

// signalZero: killing (and probing) a pid is not supported off Unix; the
// fake only has to build here, so every pid counts as gone.
func signalZero(pid int) error { return errors.New("not supported on this system") }
