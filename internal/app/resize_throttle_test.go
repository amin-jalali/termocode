package app

import (
	"testing"
	"time"
)

// A fast splitter drag (one motion event per ms for 300 ms) sends no nvim
// resize until the drag ends; then exactly one, with the final size.
func TestResizeThrottleDefersDrag(t *testing.T) {
	var th resizeThrottle
	t0 := time.Unix(0, 0)
	sent, lastW := 0, 0
	for i := 0; i < 300; i++ {
		now := t0.Add(time.Duration(i) * time.Millisecond)
		if th.request(100+i, 40, true, now) {
			sent++
		}
		if i%50 == 49 { // idle-check ticks while the pointer keeps moving
			if _, _, ok := th.flush(dragIdleResize, now); ok {
				sent++
			}
		}
	}
	if sent != 0 {
		t.Fatalf("%d resizes during an active drag, want 0", sent)
	}
	if w, _, ok := th.flush(0, t0.Add(301*time.Millisecond)); ok {
		sent++
		lastW = w
	}
	if sent != 1 || lastW != 399 {
		t.Errorf("after release: sent=%d width=%d, want one resize to 399", sent, lastW)
	}
}

func TestResizeThrottleIdleDuringDrag(t *testing.T) {
	var th resizeThrottle
	t0 := time.Unix(0, 0)
	th.request(90, 30, true, t0)
	if _, _, ok := th.flush(dragIdleResize, t0.Add(100*time.Millisecond)); ok {
		t.Fatal("resize before the pointer rested")
	}
	if w, _, ok := th.flush(dragIdleResize, t0.Add(dragIdleResize)); !ok || w != 90 {
		t.Fatal("a resting pointer should get one resize")
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
	if _, _, ok := th.flush(0, now); ok {
		t.Error("nothing should be pending")
	}
}
