// Package clock is the wall-clock seam for text the user sees ("3 days ago",
// recent-file ages). Display code calls clock.Now instead of time.Now so the
// golden screen tests (internal/devtools/shotkit) can freeze time. Timers,
// debounces and deadlines keep using time.Now directly.
package clock

import (
	"sync/atomic"
	"time"
)

// override is the frozen clock, nil for the real one. It is atomic: a test
// may swap it while a background command still reads it.
var override atomic.Pointer[func() time.Time]

// Now is the current time, or the frozen time a test or the screenshot
// tool set with Set.
func Now() time.Time {
	if f := override.Load(); f != nil {
		return (*f)()
	}
	return time.Now()
}

// Set makes Now return f() until the returned restore func runs (pass it
// to t.Cleanup). f == nil means the real clock.
func Set(f func() time.Time) (restore func()) {
	var p *func() time.Time
	if f != nil {
		p = &f
	}
	old := override.Swap(p)
	return func() { override.Store(old) }
}

// Since is time.Since against Now.
func Since(t time.Time) time.Duration { return Now().Sub(t) }
