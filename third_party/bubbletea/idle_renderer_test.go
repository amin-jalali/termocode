package tea

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// termocode patch: with nothing new to draw the ticker stops, and the
// next write or repaint still reaches the terminal.
func TestRendererIdlesAndWakes(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.start()
	defer r.kill()
	isIdle := func() bool {
		r.mtx.Lock()
		defer r.mtx.Unlock()
		return r.idle
	}
	waitFor := func(what string, f func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !f() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	r.write("first frame")
	waitFor("first frame", func() bool { return strings.Contains(out.String(), "first frame") })
	waitFor("idle", isIdle)
	r.write("second frame")
	waitFor("second frame", func() bool { return strings.Contains(out.String(), "second frame") })
	waitFor("idle again", isIdle)
	// After a repaint (resize) the same frame is drawn again.
	n := len(out.String())
	r.mtx.Lock()
	r.repaint()
	r.mtx.Unlock()
	r.write("second frame")
	waitFor("repaint", func() bool { return len(out.String()) > n })
}

// termocode patch (PERF-2): a frame after an idle stop goes out at once,
// not one frame interval later (a key typed after a pause showed up to
// 16 ms late); a second frame in the same interval still waits for the
// tick, and an unchanged frame does not wake the ticker.
func TestRendererFlushesTheFirstFrameAfterIdleAtOnce(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 2).(*standardRenderer) // 500 ms per frame
	r.start()
	defer r.kill()
	isIdle := func() bool {
		r.mtx.Lock()
		defer r.mtx.Unlock()
		return r.idle
	}
	within := func(d time.Duration, f func() bool) bool {
		deadline := time.Now().Add(d)
		for !f() {
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(2 * time.Millisecond)
		}
		return true
	}
	r.write("first frame")
	if !within(3*time.Second, isIdle) {
		t.Fatal("renderer never went idle")
	}
	r.write("first frame") // unchanged
	if !isIdle() {
		t.Error("an unchanged frame woke the ticker")
	}
	r.write("after a pause")
	if !within(100*time.Millisecond, func() bool { return strings.Contains(out.String(), "after a pause") }) {
		t.Fatal("the first frame after idle waited for the tick")
	}
	r.write("right after")
	if within(100*time.Millisecond, func() bool { return strings.Contains(out.String(), "right after") }) {
		t.Error("a second frame in the same interval went out before the tick")
	}
	if !within(2*time.Second, func() bool { return strings.Contains(out.String(), "right after") }) {
		t.Fatal("the second frame never went out")
	}
}
