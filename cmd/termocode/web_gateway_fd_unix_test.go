//go:build unix

package main

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// The handoff is fd 3 only in a gateway the UI started. A gateway refused
// for its arguments never takes fd 3: an *os.File for it would close the
// process's own fd 3 when the garbage collector runs its finalizer (in a
// test binary that is the runtime's poller: "kevent on fd 3 failed").
func TestWebGatewayBadArgsLeaveFd3Open(t *testing.T) {
	var st syscall.Stat_t
	if syscall.Fstat(3, &st) != nil { // free: a pipe of this test takes it (the lowest free fd)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		if r.Fd() != 3 && w.Fd() != 3 {
			t.Skipf("fd 3 is neither free nor ours (pipe got %d, %d)", r.Fd(), w.Fd())
		}
	}
	if run([]string{webGatewayArg, "--bogus"}) != 2 {
		t.Fatal("web gateway with bad args")
	}
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Fstat(3, &st); err != nil {
		t.Fatalf("fd 3 was closed by the refused gateway: %v", err)
	}
}
