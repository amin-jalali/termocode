package tea

import (
	"strings"
	"testing"
)

// termocode patch: the first frame drawn at the window's size goes out at
// once, not on the next tick (up to a frame interval later at startup).
// Later frames still wait for the tick.
func TestRendererFlushesFirstSizedFrameAtOnce(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 1).(*standardRenderer) // one tick a second
	r.start()
	defer r.kill()

	r.write("loading")
	if strings.Contains(out.String(), "loading") {
		t.Fatal("a frame before the window size is known went out at once")
	}
	r.handleMessages(WindowSizeMsg{Width: 40, Height: 5})
	r.write("first frame")
	if !strings.Contains(out.String(), "first frame") {
		t.Fatalf("first sized frame not written at once; out = %q", out.String())
	}
	r.write("second frame")
	if strings.Contains(out.String(), "second frame") {
		t.Fatal("a later frame went out without waiting for the tick")
	}
}
