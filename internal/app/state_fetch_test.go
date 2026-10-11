package app

import (
	"testing"
	"time"
)

// Spec: Given a state fetch ran 30ms ago,
// when nvim sends another redraw (and then goes quiet),
// then no fetch runs now but one trailing fetch is scheduled for the rest
// of the throttle window, and only one while it is pending.
func TestStateFetchPlanTrailingFetch(t *testing.T) {
	now := time.Now()
	if fetch, _ := stateFetchPlan(now, now.Add(-stateThrottle), false); !fetch {
		t.Error("a redraw after the window must fetch at once")
	}
	fetch, delay := stateFetchPlan(now, now.Add(-30*time.Millisecond), false)
	if fetch || delay != stateThrottle-30*time.Millisecond {
		t.Errorf("redraw inside the window: fetch=%v delay=%v, want a trailing fetch in %v",
			fetch, delay, stateThrottle-30*time.Millisecond)
	}
	if fetch, delay := stateFetchPlan(now, now.Add(-30*time.Millisecond), true); fetch || delay != 0 {
		t.Errorf("with a trailing fetch pending: fetch=%v delay=%v, want nothing new", fetch, delay)
	}
}
