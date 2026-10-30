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
