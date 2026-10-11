// Package clock is the wall-clock seam for text the user sees ("3 days ago",
// recent-file ages). Display code calls clock.Now instead of time.Now so the
// golden screen tests (internal/devtools/shotkit) can freeze time. Timers,
// debounces and deadlines keep using time.Now directly.
package clock

import "time"

// Now returns the current time. Tests may replace it.
var Now = time.Now

// Since is time.Since against Now.
func Since(t time.Time) time.Duration { return Now().Sub(t) }
