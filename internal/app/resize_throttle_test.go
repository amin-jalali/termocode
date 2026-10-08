package app

import (
	"testing"
	"time"
)

// A fast splitter drag (one motion event per ms for 300 ms) must reach
// nvim as at most ~30/s resizes, the last one carrying the final size.
func TestResizeThrottleCoalescesDrag(t *testing.T) {
	var th resizeThrottle
	t0 := time.Unix(0, 0)
	sent := 0
	lastW := 0
	for i := 0; i < 300; i++ {
		now := t0.Add(time.Duration(i) * time.Millisecond)
		if th.request(100+i, 40, true, now) {
			sent++
			lastW = 100 + i
		}
		// The queued flush tick fires every resizeInterval.
		if i%33 == 32 {
			if w, _, ok := th.flush(now); ok {
				sent++
				lastW = w
			}
		}
	}
	// Drag ends: the pending size goes out.
	if w, _, ok := th.flush(t0.Add(301 * time.Millisecond)); ok {
		sent++
		lastW = w
	}
	if sent > 20 {
		t.Errorf("sent %d resizes for a 300 ms drag, want ≤ 20", sent)
	}
	if lastW != 399 {
		t.Errorf("final resize width = %d, want 399 (latest size)", lastW)
	}
}

func TestResizeThrottleNotDraggingSendsAtOnce(t *testing.T) {
	var th resizeThrottle
	now := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		if !th.request(80+i, 24, false, now) {
			t.Fatal("outside a drag every resize must go out immediately")
		}
	}
	if _, _, ok := th.flush(now); ok {
		t.Error("nothing should be pending")
	}
}

func BenchmarkResizeThrottleRequest(b *testing.B) {
	var th resizeThrottle
	t0 := time.Unix(0, 0)
	for i := 0; i < b.N; i++ {
		th.request(i%200, 40, true, t0.Add(time.Duration(i)*time.Microsecond))
	}
}
