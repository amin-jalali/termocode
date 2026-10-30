//go:build unix

package webgw

import (
	"testing"
	"time"
)

func TestBucketBurstThenRate(t *testing.T) {
	b := NewBucket(20, 20)
	for i := 0; i < 20; i++ {
		if !b.Allow(t0) {
			t.Fatalf("request %d of the burst refused", i)
		}
	}
	if b.Allow(t0) {
		t.Fatal("request over the burst allowed")
	}
	// 50 ms at 20/s refills one.
	if !b.Allow(t0.Add(50 * time.Millisecond)) {
		t.Fatal("no refill after 50 ms")
	}
	if b.Allow(t0.Add(50 * time.Millisecond)) {
		t.Fatal("refilled more than one")
	}
	// A long pause refills to the burst, never above it.
	later := t0.Add(time.Hour)
	n := 0
	for b.Allow(later) {
		n++
	}
	if n != 20 {
		t.Fatalf("after a long pause %d allowed, want 20", n)
	}
}

// TestBucketClockBackwards: a clock that jumps back never adds tokens.
func TestBucketClockBackwards(t *testing.T) {
	b := NewBucket(1, 1)
	if !b.Allow(t0) {
		t.Fatal("first refused")
	}
	if b.Allow(t0.Add(-time.Hour)) {
		t.Fatal("going back in time refilled the bucket")
	}
	if b.Allow(t0.Add(500 * time.Millisecond)) {
		t.Fatal("half a token allowed")
	}
	if !b.Allow(t0.Add(time.Second)) {
		t.Fatal("no refill after 1 s")
	}
}
