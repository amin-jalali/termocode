package nvim

import (
	"testing"
	"time"
)

// Spec: Given a FetchState call in flight (nvim is busy, so it waits),
// when the buffer changes and a second FetchState is asked for while the
// first one still runs,
// then the second request is not lost: one trailing refresh runs after
// the first, so the state that comes back shows the change.
func TestFetchStateQueuesTrailingRefresh(t *testing.T) {
	c := newAttachedClient(t)
	go func() {
		for range c.events { // keep redraws from filling the channel
		}
	}()
	// Keep nvim busy so the first FetchState stays in flight.
	go func() { _ = c.ExecLua(`vim.uv.sleep(400)`) }()
	time.Sleep(50 * time.Millisecond)

	type res struct {
		s   State
		err error
	}
	first := make(chan res, 1)
	go func() { s, err := c.FetchState(); first <- res{s, err} }()
	time.Sleep(50 * time.Millisecond)

	// The change is queued in nvim behind the first fetch …
	changed := make(chan error, 1)
	go func() { changed <- c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'x'})`) }()
	time.Sleep(50 * time.Millisecond)
	// … and a refresh is asked for after it.
	_, err2 := c.FetchState()

	r := <-first
	if err := <-changed; err != nil {
		t.Fatalf("change: %v", err)
	}
	if r.err != nil {
		t.Fatalf("first FetchState: %v", r.err)
	}
	if err2 == nil {
		// The second call ran itself (no overlap): nothing was dropped.
		return
	}
	if !r.s.Dirty {
		t.Errorf("second FetchState was dropped (%v) and the first returned stale state (Dirty=false): the buffer change is never reported", err2)
	}
}
