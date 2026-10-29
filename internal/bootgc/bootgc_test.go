package bootgc

import (
	"runtime/debug"
	"testing"
	"time"
)

// gcPercent reads the current GC percent without changing it.
func gcPercent() int {
	p := debug.SetGCPercent(100)
	debug.SetGCPercent(p)
	return p
}

func TestHoldUntilRelease(t *testing.T) {
	defer debug.SetGCPercent(gcPercent())
	start(func(string) string { return "" })
	if got := gcPercent(); got >= 0 {
		t.Fatalf("GC percent while held = %d, want off (<0)", got)
	}
	SetPercent(200) // the setting waits for Release
	if got := gcPercent(); got >= 0 {
		t.Fatalf("SetPercent applied while held: %d", got)
	}
	Release()
	if got := gcPercent(); got != 200 {
		t.Fatalf("GC percent after Release = %d, want 200", got)
	}
	Release() // a second Release changes nothing
	if got := gcPercent(); got != 200 {
		t.Fatalf("GC percent after a second Release = %d, want 200", got)
	}
	SetPercent(150) // after Release: at once
	if got := gcPercent(); got != 150 {
		t.Fatalf("SetPercent after Release = %d, want 150", got)
	}
}

// A GOGC in the environment is the user's choice: never held.
func TestGOGCEnvIsNotHeld(t *testing.T) {
	defer debug.SetGCPercent(gcPercent())
	debug.SetGCPercent(77)
	start(func(k string) string {
		if k == "GOGC" {
			return "77"
		}
		return ""
	})
	if got := gcPercent(); got != 77 {
		t.Fatalf("GC percent with GOGC set = %d, want 77 (untouched)", got)
	}
	if Held() {
		t.Fatal("held although GOGC is set")
	}
}

// Once startup has settled after Release, the memory startup used and
// no longer needs goes back to the OS (once).
func TestReleaseFreesStartupMemoryOnceSettled(t *testing.T) {
	defer debug.SetGCPercent(gcPercent())
	var delays []time.Duration
	freed := 0
	oldFree, oldAfter := freeOSMemory, afterFunc
	freeOSMemory = func() { freed++ }
	afterFunc = func(d time.Duration, f func()) { delays = append(delays, d); f() }
	t.Cleanup(func() { freeOSMemory, afterFunc = oldFree, oldAfter })

	start(func(string) string { return "" })
	if freed != 0 {
		t.Fatal("memory freed while still held")
	}
	Release()
	if freed != 1 || len(delays) != 1 || delays[0] != settleDelay {
		t.Fatalf("after Release: freed %d times, delays %v; want once after %v", freed, delays, settleDelay)
	}
	Release()
	if freed != 1 {
		t.Fatalf("a second Release freed memory again (%d)", freed)
	}
}
