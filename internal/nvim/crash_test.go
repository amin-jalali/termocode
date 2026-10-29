package nvim

import (
	"testing"
	"time"
)

// TestServeFailedEndsTheConnection: after a recovered panic in serve the
// client counts as exited (Next reports it, the app offers Restart);
// twice is fine.
func TestServeFailedEndsTheConnection(t *testing.T) {
	c := &Client{exited: make(chan struct{})}
	c.serveFailed()
	c.serveFailed()
	if !c.Exited() || c.exitErr == nil {
		t.Fatalf("exited %v, err %v", c.Exited(), c.exitErr)
	}
}

// TestUnblockedSurvivesAPanic: a panic in the call gives an error, not a
// hang or the end of termocode.
func TestUnblockedSurvivesAPanic(t *testing.T) {
	c := &Client{exited: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- c.unblocked(func() error { panic("test") }) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("no error after a panic")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the call hangs after a panic")
	}
}
