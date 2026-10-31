//go:build !unix

package main

// agentSocketOfCwd: no session folder off unix.
func agentSocketOfCwd() string { return "" }
