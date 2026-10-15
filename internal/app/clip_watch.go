package app

import (
	"bytes"
	"context"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// clipWatcher feeds copies made in other apps into the clipboard ring.
// The clipboard library's own Watch reads every second; on X11 each read
// opens a new connection and makes the app that owns the clipboard send
// its whole content, forever. Once the terminal has sent a focus event,
// the watcher reads only when termocode gets focus back: a copy made
// elsewhere is in the clipboard by then. A terminal without focus events
// keeps the timer.
type clipWatcher struct {
	read     func() []byte
	push     func(string)
	interval time.Duration
	// focusEvents: the terminal reports focus, so the timer is off.
	focusEvents atomic.Bool
	kick        chan struct{}
}

func newClipWatcher(read func() []byte, push func(string), interval time.Duration) *clipWatcher {
	return &clipWatcher{read: read, push: push, interval: interval, kick: make(chan struct{}, 1)}
}

// focus records a focus event from the terminal; gained asks for a read.
func (w *clipWatcher) focus(gained bool) {
	w.focusEvents.Store(true)
	if gained {
		select {
		case w.kick <- struct{}{}:
		default:
		}
	}
}

func (w *clipWatcher) run(ctx context.Context) {
	last := w.read()
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-t.C:
			if w.focusEvents.Load() {
				t.Stop() // focus events from now on; kicks wake us
				continue
			}
		}
		b := w.read()
		if b != nil && !bytes.Equal(b, last) {
			last = b
			w.push(string(b))
		}
	}
}

// activeClipWatcher is the running watcher (nil when there is none), so
// focus messages can reach it.
var activeClipWatcher atomic.Pointer[clipWatcher]

// clipFocus passes a focus change to the watcher.
func clipFocus(msg tea.Msg) {
	w := activeClipWatcher.Load()
	if w == nil {
		return
	}
	switch msg.(type) {
	case tea.FocusMsg:
		w.focus(true)
	case tea.BlurMsg:
		w.focus(false)
	}
}
