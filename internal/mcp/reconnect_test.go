package mcp

import (
	"testing"
	"time"
)

// Spec (RT-6): the reconnect delay doubles per try, never passes the
// cap, and is jittered into [d/2, d] so many sessions do not knock at
// the same moment.
func TestBackoffDoublesWithJitterAndCap(t *testing.T) {
	base, limit := time.Second, 30*time.Second
	low := func(int64) int64 { return 0 }
	high := func(n int64) int64 { return n - 1 }
	for try, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		if got := Backoff(try, base, limit, high); got < want-time.Nanosecond || got > want {
			t.Errorf("try %d high: %v, want ~%v", try, got, want)
		}
		if got := Backoff(try, base, limit, low); got != want/2 {
			t.Errorf("try %d low: %v, want %v", try, got, want/2)
		}
	}
	if got := Backoff(200, base, limit, high); got > limit || got <= 0 {
		t.Errorf("huge try: %v", got)
	}
	if got := Backoff(3, base, limit, nil); got < 4*time.Second || got > 8*time.Second {
		t.Errorf("default random: %v", got)
	}
}
