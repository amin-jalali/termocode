package testkit

import (
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Helpers for the huge-input scenarios (SCN-12). Each bound is about 3×
// what the step took on a developer machine (noted next to it), so busy
// CI runners and -race pass while a step that walks everything on the
// UI loop, or keeps a copy of a huge input, fails.

// heapMB runs the GC and returns the live Go heap in MB.
func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.HeapAlloc) / (1 << 20)
}

// raceFactor widens time bounds under the race detector (it slows Go
// code down 2–10×).
func raceFactor() time.Duration {
	if raceOn() {
		return 4
	}
	return 1
}

// within fails the test when took is over bound (scaled for -race).
func within(t testing.TB, what string, took, bound time.Duration) {
	t.Helper()
	bound *= raceFactor()
	t.Logf("%s: %v (bound %v)", what, took.Round(time.Millisecond), bound)
	if took > bound {
		t.Errorf("%s took %v, bound %v", what, took.Round(time.Millisecond), bound)
	}
}

// heapWithin fails the test when the Go heap grew more than boundMB
// since baseMB.
func heapWithin(t testing.TB, what string, baseMB, boundMB float64) {
	t.Helper()
	got := heapMB() - baseMB
	t.Logf("%s: heap +%.1f MB (bound %.0f MB)", what, got, boundMB)
	if got > boundMB {
		t.Errorf("%s: heap grew %.1f MB, bound %.0f MB", what, got, boundMB)
	}
}

// keyTime is how long one key blocks the UI loop (Update + View); the
// driver then settles.
func keyTime(d *Driver, key string) time.Duration {
	d.T.Helper()
	took := d.Timed(KeyMsg(key))
	d.Settle(d.opt.MaxSettle)
	return took
}

// worstKey presses each key and returns the longest UI-loop time.
func worstKey(d *Driver, keys ...string) time.Duration {
	d.T.Helper()
	var worst time.Duration
	for _, k := range keys {
		worst = max(worst, keyTime(d, k))
	}
	return worst
}

// skipShortScale skips a slow scale scenario under -short (CI's fast
// job); the non-short job runs it.
func skipShortScale(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("huge input: runs without -short")
	}
}

// wheel is a wheel notch at x, y (for Driver.Timed).
func wheel(x, y int, up bool) tea.MouseMsg {
	if up {
		return tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelUp, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	}
	return tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelDown, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
}
