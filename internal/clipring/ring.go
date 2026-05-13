// Package clipring stores a bounded ring of recent clipboard captures.
//
// Both nvim's TextYankPost autocmd and the system-wide clipboard.Watch()
// stream feed into this ring; the picker UI reads it via Items().
package clipring

import (
	"sync"
	"time"
)

// Entry is one captured clipboard payload.
type Entry struct {
	Content    string
	CapturedAt time.Time
}

const (
	defaultCap = 20
	maxBytes   = 64 * 1024 // 64 KB cap per entry
)

// Ring is a thread-safe bounded ring of clipboard captures, newest-first.
type Ring struct {
	mu    sync.Mutex
	items []Entry
	cap   int
}

// New returns an empty ring with the default capacity (20).
func New() *Ring { return &Ring{cap: defaultCap} }

// Push records a fresh clipboard payload. Empty strings, oversize payloads,
// and consecutive duplicates of the most-recent entry are silently dropped.
func (r *Ring) Push(content string) {
	if content == "" || len(content) > maxBytes {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) > 0 && r.items[0].Content == content {
		return
	}
	e := Entry{Content: content, CapturedAt: time.Now()}
	if len(r.items) >= r.cap {
		r.items = r.items[:r.cap-1]
	}
	r.items = append([]Entry{e}, r.items...)
}

// Items returns a snapshot of the ring, newest-first.
func (r *Ring) Items() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.items))
	copy(out, r.items)
	return out
}

// Len returns the number of stored entries.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}
