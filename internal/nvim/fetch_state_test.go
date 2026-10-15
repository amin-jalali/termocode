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

// Spec (benchmarks 2026-10-15): FetchState runs after every cursor move, so
// it must not walk the current line. On a 20 MB one-line file (minified
// JSON) with termocode's options (breakindent, linebreak) and the cursor at
// the end, virtcol took ~0.2–0.5 s per call and key presses piled up for
// tens of seconds. Short lines still get the display column (tabs count).
func TestFetchStateFastOnHugeLine(t *testing.T) {
	c := newAttachedClient(t)
	go func() {
		for range c.events {
		}
	}()
	if err := c.ExecLua(`
		vim.o.breakindent, vim.o.linebreak = true, true
		vim.api.nvim_buf_set_lines(0, 0, -1, false, { string.rep('{"a":1},', 20 * 1024 * 1024 / 8) })
		vim.cmd('normal! $')`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchState(); err != nil { // warm up
		t.Fatal(err)
	}
	start := time.Now()
	s, err := c.FetchState()
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("FetchState took %v at the end of a 20 MB line, want < 100ms", d)
	}
	if s.CursorCol < 20*1024*1024-8 {
		t.Errorf("column %d, want about the line length", s.CursorCol)
	}

	if err := c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, { '\tx' }); vim.cmd('normal! $')`); err != nil {
		t.Fatal(err)
	}
	s, err = c.FetchState()
	if err != nil {
		t.Fatal(err)
	}
	if want := 9; s.CursorCol != want { // tab = 8 display columns, then x
		t.Errorf("short line: display column %d, want %d", s.CursorCol, want)
	}
}
