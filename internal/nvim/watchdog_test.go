package nvim

import (
	"regexp"
	"strconv"
	"testing"
)

// The watchdog timer wakes nvim on every tick, also while nothing
// happens. Its deadlines are 1.5 s and 3 s, so a tick each second is
// enough (PERF-4-2: was 250 ms, 4 idle wakeups per second).
func TestWatchdogTicksAtMostOncePerSecond(t *testing.T) {
	m := regexp.MustCompile(`t:start\((\d+), (\d+),`).FindStringSubmatch(watchdogLua)
	if m == nil {
		t.Fatal("watchdogLua has no t:start(first, repeat, ...) call")
	}
	if rep, _ := strconv.Atoi(m[2]); rep < 1000 {
		t.Errorf("watchdog repeats every %d ms, want >= 1000 ms", rep)
	}
}
