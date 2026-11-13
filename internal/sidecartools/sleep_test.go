package sidecartools

import (
	"context"
	"testing"
	"time"
)

// RC-8: tc-sleep keeps the tools sidecar alive doing nothing until the
// engine stops it (SIGTERM), then exits 0 at once.
func TestSleepReturnsWhenStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- Sleep(ctx) }()
	select {
	case <-done:
		t.Fatal("Sleep returned before it was stopped")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not return after the stop")
	}
}
