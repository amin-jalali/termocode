package app

import (
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The hub's own rate check (web design 3.5): the gateway enforces each
// op's rate; the hub checks it again with 2x headroom, so a bug in one
// does not open the other. Over it, a request is refused with "limit".
// The buckets live on the connection's reader goroutine.

// webHubReqPerSec and webHubReqBurst cap all requests of one connection
// (the gateway's 20/s, burst 40, with 2x headroom).
const (
	webHubReqPerSec = 40
	webHubReqBurst  = 80
)

// webBucket is a token bucket in exact time (GCRA): burst at once, then
// n per period on average. A clock that goes back adds no room.
type webBucket struct {
	interval  time.Duration
	tolerance time.Duration
	tat       time.Time
}

func newWebBucket(n int, per time.Duration) *webBucket {
	n = max(n, 1)
	iv := max(per/time.Duration(n), 1)
	return &webBucket{interval: iv, tolerance: iv * time.Duration(n-1)}
}

// room reports whether units fit at now (without taking them).
func (b *webBucket) room(now time.Time, units int) bool {
	tat := b.tat
	if tat.Before(now) {
		tat = now
	}
	return tat.Add(b.interval*time.Duration(units)).Sub(now) <= b.tolerance+b.interval
}

func (b *webBucket) take(now time.Time, units int) {
	if b.tat.Before(now) {
		b.tat = now
	}
	b.tat = b.tat.Add(b.interval * time.Duration(units))
}

// webRates are one connection's buckets (reader goroutine only).
type webRates struct {
	all   *webBucket
	ops   map[string]*webBucket
	bytes map[string]*webBucket
}

// allow takes one call of p (and size bytes for a byte-capped op) if
// every bucket has room at now.
func (r *webRates) allow(p webproto.Policy, size int, now time.Time) bool {
	if r.all == nil {
		r.all = newWebBucket(webHubReqBurst, webHubReqBurst*time.Second/webHubReqPerSec)
		r.ops, r.bytes = map[string]*webBucket{}, map[string]*webBucket{}
	}
	op := r.ops[p.Op]
	if op == nil {
		op = newWebBucket(2*p.Rate.N, p.Rate.Per)
		r.ops[p.Op] = op
	}
	var bb *webBucket
	if p.BytesPerSec > 0 {
		if bb = r.bytes[p.Op]; bb == nil {
			bb = newWebBucket(2*p.BytesPerSec, time.Second)
			r.bytes[p.Op] = bb
		}
	}
	if !r.all.room(now, 1) || !op.room(now, 1) || (bb != nil && !bb.room(now, size)) {
		return false
	}
	r.all.take(now, 1)
	op.take(now, 1)
	if bb != nil {
		bb.take(now, size)
	}
	return true
}
