// Package debounce coalesces bursts of noisy events (nvim notifications,
// keystrokes, file-watcher pings) into one Bubble Tea message per quiet
// period.
//
// Usage from an Update handler:
//
//	return m, m.debounce.Do("ai-inline", 300*time.Millisecond, func() tea.Msg {
//		return inlineRequestMsg{...}
//	})
//
// Each Do call for a key supersedes the previous one. Only the call that is
// still the newest when its delay expires produces a message; superseded
// timers resolve to nil, which Bubble Tea ignores.
package debounce

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Debouncer tracks one generation counter per key. The zero value is ready
// to use; it is safe for concurrent use. Share it by pointer.
type Debouncer struct {
	mu  sync.Mutex
	gen map[string]uint64
}

// New returns an empty Debouncer.
func New() *Debouncer { return &Debouncer{} }

// Do schedules fn to run after `delay` unless another Do (or Cancel) for
// the same key happens first. fn runs on a Bubble Tea command goroutine
// when the timer fires, so it should only build a message — keep model
// mutation in Update. A nil Debouncer runs fn after the delay without
// coalescing.
func (d *Debouncer) Do(key string, delay time.Duration, fn func() tea.Msg) tea.Cmd {
	if fn == nil {
		return nil
	}
	if d == nil {
		return tea.Tick(delay, func(time.Time) tea.Msg { return fn() })
	}
	g := d.bump(key)
	return tea.Tick(delay, func(time.Time) tea.Msg {
		if !d.Current(key, g) {
			return nil
		}
		return fn()
	})
}

// Cancel drops any pending Do for key (its timer still fires but yields
// nil).
func (d *Debouncer) Cancel(key string) {
	if d == nil {
		return
	}
	d.bump(key)
}

// Current reports whether generation g is still the newest for key. Useful
// for callers that schedule their own timers via Next.
func (d *Debouncer) Current(key string, g uint64) bool {
	if d == nil {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.gen[key] == g
}

// Next starts a new generation for key and returns it — the building block
// behind Do, exposed for callers that need the raw counter (e.g. to tag an
// async request and discard stale replies).
func (d *Debouncer) Next(key string) uint64 {
	if d == nil {
		return 0
	}
	return d.bump(key)
}

func (d *Debouncer) bump(key string) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gen == nil {
		d.gen = map[string]uint64{}
	}
	d.gen[key]++
	return d.gen[key]
}
