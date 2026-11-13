package clock

import (
	"testing"
	"time"
)

// Since measures against the frozen clock.
func TestSinceUsesTheFrozenClock(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	t.Cleanup(Set(func() time.Time { return now }))
	if got := Since(now.Add(-90 * time.Minute)); got != 90*time.Minute {
		t.Fatalf("Since = %v", got)
	}
}
