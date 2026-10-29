// Package bootgc keeps the garbage collector off while termocode starts.
//
// Package init and the build of the first frame allocate a few MB that
// stay live, and the default GC (GOGC=100, 4 MB first goal) ran one or
// two full cycles in that time, on the path to the first frame. With
// the collector off until Release, startup does no GC work at all; the
// heap at that point is small (~10 MB) and the memory limit set in main
// still bounds it. Release turns the collector on with the percent from
// SetPercent: the UI calls it once its first frame is drawn, every other
// command (session client and server, mcp, …) at once. A safety timer
// releases it anyway after maxHold.
//
// Import it from main so its init runs before the big package inits.
// GOGC in the environment is the user's choice: then nothing is held.
package bootgc

import (
	"os"
	"runtime/debug"
	"sync"
	"time"
)

// maxHold releases the collector even when nobody calls Release.
const maxHold = 3 * time.Second

var (
	mu      sync.Mutex
	held    bool
	percent = 100 // Go's default, until SetPercent
)

func init() {
	start(os.Getenv)
	time.AfterFunc(maxHold, Release)
}

// start turns the collector off unless GOGC is set.
func start(getenv func(string) string) {
	mu.Lock()
	defer mu.Unlock()
	if getenv("GOGC") != "" {
		held = false
		return
	}
	held = true
	debug.SetGCPercent(-1)
}

// SetPercent sets the GC percent: at Release while held, else at once.
func SetPercent(p int) {
	mu.Lock()
	defer mu.Unlock()
	percent = p
	if !held {
		debug.SetGCPercent(p)
	}
}

// Release turns the collector on (once; later calls do nothing).
func Release() {
	mu.Lock()
	defer mu.Unlock()
	if !held {
		return
	}
	held = false
	debug.SetGCPercent(percent)
}

// Held reports whether the collector is still held off.
func Held() bool {
	mu.Lock()
	defer mu.Unlock()
	return held
}
