package ai

import (
	"context"
	"sync"
)

// Side requests of a turn (look_at's look at an image, a summary of the
// older chat) are counted into the turn's usage: the turn's context
// carries a sink, the side request adds to it, and Agent.Run adds what
// the sink holds to the usage of its EventDone or EventError.

type sideUsageKey struct{}

type sideUsage struct {
	mu      sync.Mutex
	u       *Usage
	claimed bool // an Agent.Run counts it
}

// WithSideUsage returns ctx with a sink for the side requests of the turn
// that runs in it. Requests added before Agent.Run (a summary before the
// turn) go into that turn's total.
func WithSideUsage(ctx context.Context) context.Context {
	return context.WithValue(ctx, sideUsageKey{}, &sideUsage{})
}

// AddSideUsage counts u as a side request of ctx's turn (nothing without
// a sink). Its model and its prompt size are left out: the turn's model
// and its context window use stay the main model's.
func AddSideUsage(ctx context.Context, u *Usage) {
	s, _ := ctx.Value(sideUsageKey{}).(*sideUsage)
	if s == nil || u == nil {
		return
	}
	c := *u
	c.Model, c.PeakContext = "", 0
	s.mu.Lock()
	s.u = s.u.Add(&c)
	s.mu.Unlock()
}

// claimSideUsage gives Agent.Run its sink: the one in ctx when no other
// Run counts it yet, else a new one (a nested turn counts its own).
func claimSideUsage(ctx context.Context) (context.Context, *sideUsage) {
	if s, _ := ctx.Value(sideUsageKey{}).(*sideUsage); s != nil {
		s.mu.Lock()
		free := !s.claimed
		s.claimed = true
		s.mu.Unlock()
		if free {
			return ctx, s
		}
	}
	s := &sideUsage{claimed: true}
	return context.WithValue(ctx, sideUsageKey{}, s), s
}

// take returns the side usage so far and empties the sink.
func (s *sideUsage) take() *Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.u
	s.u = nil
	return u
}
