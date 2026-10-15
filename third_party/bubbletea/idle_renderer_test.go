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
