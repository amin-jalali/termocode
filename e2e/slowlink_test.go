//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// SlowLink is a network between the terminal and termocode, like SSH over
// a slow line: every byte arrives Latency after it was sent, and no faster
// than BytesPerSec (0: no limit). The sender side never blocks, like a TCP
// send buffer, so a program that writes faster than the line builds up a
// queue, which a user sees as run-on (Group S2). No root needed (tc netem
// is not).
type SlowLink struct {
	Latency     time.Duration
	BytesPerSec int
}

// linkDir is one direction of a SlowLink.
type linkDir struct {
	l       *SlowLink
	deliver func([]byte)
	ch      chan linkChunk
	mu      sync.Mutex
	free    time.Time // when the line is free to send the next byte
	closed  bool
	once    sync.Once
	done    chan struct{}
}

type linkChunk struct {
	p  []byte
	at time.Time
}

func (l *SlowLink) dir(deliver func([]byte)) *linkDir {
	d := &linkDir{l: l, deliver: deliver, ch: make(chan linkChunk, 1<<16), done: make(chan struct{})}
	go func() {
		defer close(d.done)
		for c := range d.ch {
			if w := time.Until(c.at); w > 0 {
				time.Sleep(w)
			}
			d.deliver(c.p)
		}
	}()
	return d
}

// Write queues p; it arrives after the line has sent it, plus Latency.
func (d *linkDir) Write(p []byte) {
	if d == nil || len(p) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	now := time.Now()
	start := now
	if d.free.After(start) {
		start = d.free
	}
	end := start
	if d.l.BytesPerSec > 0 {
		end = start.Add(time.Duration(len(p)) * time.Second / time.Duration(d.l.BytesPerSec))
	}
	d.free = end
	d.ch <- linkChunk{p: append([]byte(nil), p...), at: end.Add(d.l.Latency)}
}

// Close stops the link (pending bytes are dropped).
func (d *linkDir) Close() {
	if d == nil {
		return
	}
	d.once.Do(func() {
		d.mu.Lock()
		d.closed = true
		close(d.ch)
		d.mu.Unlock()
	})
}

// rawLen is how many bytes of output the terminal has received.
func (tm *Term) rawLen() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.raw.Len()
}

// rawSince returns the output received after byte offset off.
func (tm *Term) rawSince(off int) []byte {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return append([]byte(nil), tm.raw.Bytes()[off:]...)
}

// lastChange watches the screen for window and returns when it last
// changed (the zero time when it did not change at all).
func (tm *Term) lastChange(window time.Duration) time.Time {
	var last time.Time
	prev := tm.Screen()
	for end := time.Now().Add(window); time.Now().Before(end); {
		time.Sleep(5 * time.Millisecond)
		if cur := tm.Screen(); cur != prev {
			prev, last = cur, time.Now()
		}
	}
	return last
}

var lnRe = regexp.MustCompile(`Ln (\d+)`)

// statusLine returns the cursor line the status bar shows (0: none).
func (tm *Term) statusLine() int {
	ls := tm.Lines()
	m := lnRe.FindStringSubmatch(ls[len(ls)-1])
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// linkStats is what one burst of input cost on the line.
type linkStats struct {
	frames, bytes int
}

func (s linkStats) String() string {
	per := 0
	if s.frames > 0 {
		per = s.bytes / s.frames
	}
	return fmt.Sprintf("%d frames, %d bytes, %d bytes/frame", s.frames, s.bytes, per)
}

// statsSince counts the frames (each starts with cursor home, \e[H) and
// bytes received after byte offset off.
func (tm *Term) statsSince(off int) linkStats {
	b := tm.rawSince(off)
	return linkStats{frames: bytes.Count(b, []byte("\x1b[H")), bytes: len(b)}
}

// slowLinkBoot starts termocode over a slow link, as over SSH (persistent
// session on, like the default there), with a long file open.
func slowLinkBoot(t *testing.T, link *SlowLink) *Term {
	t.Helper()
	r := newSessionRig(t)
	var b strings.Builder
	for i := 1; i <= 400; i++ {
		fmt.Fprintf(&b, "line %03d: the quick brown fox jumps over the lazy dog → 界\n", i)
	}
	if err := os.WriteFile(filepath.Join(r.ws, "long.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Link: link})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenFile("long.txt")
	tm.WaitText("line 001:", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	return tm
}

// Spec (Group S2): over a slow link (150 ms each way, with and without a
// bandwidth limit) a held arrow key, the mouse wheel and fast typing stop
// as soon as the user stops: the screen settles within the round trip
// plus a little, never keeps moving, and the cursor ends where the keys
// put it. The bytes per frame are logged (go test -v).
func TestSlowLink(t *testing.T) {
	const latency = 150 * time.Millisecond
	// The user sees the last key's effect one round trip after it; allow
	// processing and a few frames on top.
	const slack = 700 * time.Millisecond
	links := []struct {
		name string
		link SlowLink
	}{
		{"150ms", SlowLink{Latency: latency}},
		{"150ms-1Mbit", SlowLink{Latency: latency, BytesPerSec: 125_000}},
	}
	for _, lc := range links {
		t.Run(lc.name, func(t *testing.T) {
			link := lc.link
			tm := slowLinkBoot(t, &link)

			// Hold Down: 30 key repeats a second for 2 s.
			const repeats = 60
			off := tm.rawLen()
			for i := 0; i < repeats; i++ {
				tm.Send(KeyDown)
				time.Sleep(33 * time.Millisecond)
			}
			release := time.Now()
			last := tm.lastChange(3 * time.Second)
			st := tm.statsSince(off)
			t.Logf("arrows: %s; last change %v after release", st, last.Sub(release).Round(time.Millisecond))
			if got := tm.statusLine(); got != 1+repeats {
				t.Errorf("arrows: cursor on line %d, want %d", got, 1+repeats)
			}
			if lag := last.Sub(release); lag > 2*latency+slack {
				t.Errorf("arrows: screen kept changing %v after release (run-on)", lag)
			}

			// Wheel down over the editor: 60 notches a second for 1 s.
			off = tm.rawLen()
			cols, rows := tm.Size()
			for i := 0; i < 60; i++ {
				tm.Send(fmt.Sprintf("\x1b[<65;%d;%dM", cols/2, rows/2))
				time.Sleep(16 * time.Millisecond)
			}
			release = time.Now()
			last = tm.lastChange(3 * time.Second)
			t.Logf("wheel: %s; last change %v after release", tm.statsSince(off), last.Sub(release).Round(time.Millisecond))
			if lag := last.Sub(release); lag > 2*latency+slack {
				t.Errorf("wheel: screen kept changing %v after release (run-on)", lag)
			}

			// Type fast (40 characters a second) on a new line.
			tm.Send("o")
			tm.settle()
			text := "typed over a slow link → 界 0123456789 abcdefghij"
			off = tm.rawLen()
			for _, r := range text {
				tm.Send(string(r))
				time.Sleep(25 * time.Millisecond)
			}
			release = time.Now()
			last = tm.lastChange(3 * time.Second)
			t.Logf("typing: %s; last change %v after release", tm.statsSince(off), last.Sub(release).Round(time.Millisecond))
			if !tm.Has(text) {
				t.Errorf("typed text not on screen:\n%s", tm.Screen())
			}
			if lag := last.Sub(release); lag > 2*latency+slack {
				t.Errorf("typing: screen kept changing %v after release (run-on)", lag)
			}
			tm.Send(KeyEsc)
			tm.settle()  // Esc then u at once would read as Alt+U
			tm.Send("u") // undo the typed line: no unsaved-changes prompt
			tm.settle()
			tm.Quit()
		})
	}
}
