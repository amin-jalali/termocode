package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The clipboard library's Watch read the X clipboard every second: each
// read opens an X connection and makes the app that owns the clipboard
// send its content, all day long. With focus events from the terminal the
// watcher reads only when termocode gets focus back (or on a kick).
func TestClipWatcherReadsOnFocusNotOnTimer(t *testing.T) {
	var reads atomic.Int64
	var mu sync.Mutex
	content := "one"
	var pushed []string
	w := newClipWatcher(func() []byte {
		reads.Add(1)
		mu.Lock()
		defer mu.Unlock()
		return []byte(content)
	}, func(s string) {
		mu.Lock()
		pushed = append(pushed, s)
		mu.Unlock()
	}, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.run(ctx)

	// No focus events yet (a terminal that has none): it polls.
	time.Sleep(150 * time.Millisecond)
	if n := reads.Load(); n < 3 {
		t.Fatalf("reads without focus events = %d, want the timer to poll", n)
	}
	w.focus(false)
	time.Sleep(50 * time.Millisecond)
	before := reads.Load()
	time.Sleep(200 * time.Millisecond)
	if n := reads.Load() - before; n != 0 {
		t.Errorf("reads after a focus event = %d while waiting, want 0", n)
	}
	mu.Lock()
	content = "two"
	mu.Unlock()
	w.focus(true) // back in termocode: read once
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := append([]string(nil), pushed...)
		mu.Unlock()
		if len(got) == 1 && got[0] == "two" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pushed = %q, want [two] (the content at start is not new)", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
