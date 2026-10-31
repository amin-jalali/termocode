package mcp

import (
	"math/rand/v2"
	"time"
)

// Reconnects (RT-5, RT-6, RT-9): a remote server that can not be reached
// tries again by itself, later each time; "Reconnect all" tries every
// failed server now.

// Backoff is the wait before reconnect try number try (0 first): base
// doubled per try, at most limit, then jittered into [d/2, d]. rnd(n)
// returns a number in [0, n) (nil: math/rand).
func Backoff(try int, base, limit time.Duration, rnd func(int64) int64) time.Duration {
	if rnd == nil {
		rnd = rand.Int64N
	}
	d := base
	for i := 0; i < try && d < limit; i++ {
		d *= 2
	}
	if d > limit || d <= 0 {
		d = limit
	}
	half := d / 2
	return half + time.Duration(rnd(int64(d-half)+1))
}
