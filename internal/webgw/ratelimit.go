//go:build unix

package webgw

import (
	"sync"
	"time"
)

// Bucket is a token bucket (as GCRA, in exact integer time): burst
// requests at once, then perSecond on average. The caller passes the
// time, so tests use a fake clock. A clock that jumps back never adds
// room.
type Bucket struct {
	interval  time.Duration // time one request uses up
	tolerance time.Duration // how far ahead of now the bucket may run

	mu  sync.Mutex
	tat time.Time // theoretical arrival time of the next request
}

// NewBucket allows burst requests at once and perSecond after that.
func NewBucket(perSecond float64, burst int) *Bucket {
	interval := time.Duration(float64(time.Second) / perSecond)
	if burst < 1 {
		burst = 1
	}
	return &Bucket{interval: interval, tolerance: interval * time.Duration(burst-1)}
}

// Allow takes one request from the bucket at now, or says no.
func (b *Bucket) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	tat := b.tat
	if tat.Before(now) {
		tat = now
	}
	if tat.Sub(now) > b.tolerance {
		return false
	}
	b.tat = tat.Add(b.interval)
	return true
}

// AllowN takes n requests (or n bytes) from the bucket at once, or says
// no and takes nothing.
func (b *Bucket) AllowN(now time.Time, n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	tat := b.tat
	if tat.Before(now) {
		tat = now
	}
	next := tat.Add(b.interval * time.Duration(n))
	if next.Sub(now) > b.tolerance+b.interval {
		return false
	}
	b.tat = next
	return true
}

// NextN is how long from now until AllowN(n) would pass (0: now).
func (b *Bucket) NextN(now time.Time, n int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	tat := b.tat
	if tat.Before(now) {
		tat = now
	}
	wait := tat.Add(b.interval*time.Duration(n)).Sub(now) - b.tolerance - b.interval
	if wait < 0 {
		return 0
	}
	return wait
}
