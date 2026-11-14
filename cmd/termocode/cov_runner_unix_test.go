//go:build unix

package main

import (
	"bytes"
	"syscall"
	"testing"
	"time"
)

// serve runs until it is interrupted, then returns 0. (Unix only: it
// sends itself SIGINT; moved here from cov_runner_test.go so windows
// builds.)
func TestRunnerServeStopsOnInterrupt(t *testing.T) {
	dir, folder := t.TempDir(), t.TempDir()
	done := make(chan int, 1)
	var out, errw bytes.Buffer
	go func() {
		done <- runRunnerIn([]string{"serve", "--folder", folder, "--pool", "1"}, nil, &out, &errw,
			func() (string, error) { return folder, nil }, func() (string, error) { return dir, nil })
	}()
	time.Sleep(700 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve: %d %s", code, errw.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
}
