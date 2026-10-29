package nvim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Leak tracking for tests: a test binary calls TrackClients in TestMain
// and LeakedClients at its end. Each client remembers the test function
// that started it until its nvim process is reaped, so a test that never
// closes its nvim is named. Off (and free) in the product.
var leaks struct {
	mu   sync.Mutex
	on   bool
	live map[*Client]string
}

// TrackClients turns leak tracking on (tests only).
func TrackClients() {
	leaks.mu.Lock()
	defer leaks.mu.Unlock()
	leaks.on = true
	if leaks.live == nil {
		leaks.live = map[*Client]string{}
	}
}

// track records c until its nvim process is reaped.
func track(c *Client) {
	leaks.mu.Lock()
	if !leaks.on {
		leaks.mu.Unlock()
		return
	}
	leaks.live[c] = creator()
	leaks.mu.Unlock()
	crash.Go("nvim leak tracking", func() {
		<-c.reaped
		leaks.mu.Lock()
		delete(leaks.live, c)
		leaks.mu.Unlock()
	})
}

// creator is the first Test… function on the caller's stack, else the
// innermost caller outside this package.
func creator() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	first := ""
	for {
		f, more := frames.Next()
		name := f.Function[strings.LastIndex(f.Function, "/")+1:]
		if i := strings.Index(name, ".Test"); i >= 0 {
			name = name[i+1:]
			if j := strings.IndexByte(name, '.'); j >= 0 {
				name = name[:j]
			}
			return name
		}
		if first == "" && !strings.HasPrefix(name, "nvim.") {
			first = name
		}
		if !more {
			break
		}
	}
	return first
}

// LeakedClients waits up to wait for the tracked nvim processes to be
// reaped and returns the creators of those still running, sorted.
func LeakedClients(wait time.Duration) []string {
	deadline := time.Now().Add(wait)
	for {
		leaks.mu.Lock()
		out := make([]string, 0, len(leaks.live))
		for _, who := range leaks.live {
			out = append(out, who)
		}
		leaks.mu.Unlock()
		if len(out) == 0 || time.Now().After(deadline) {
			slices.Sort(out)
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// LeakReport is LeakedClients as text for a test binary's end: "" when
// every nvim is gone, else the count and each test with its count.
func LeakReport(wait time.Duration) string {
	left := LeakedClients(wait)
	if len(left) == 0 {
		return ""
	}
	count := map[string]int{}
	for _, who := range left {
		count[who]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d nvim process(es) still running after the tests (%d nvim children); close each nvim in the test that starts it:\n", len(left), ChildProcesses())
	for _, who := range slices.Compact(left) {
		fmt.Fprintf(&b, "  %s: %d\n", who, count[who])
	}
	return b.String()
}

// ChildProcesses counts this process's children named nvim, from /proc
// (tests only; -1 where there is no /proc).
func ChildProcesses() int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return -1
	}
	self, n := strconv.Itoa(os.Getpid()), 0
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "status"))
		if err != nil {
			continue
		}
		name, ppid := "", ""
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "Name:"); ok {
				name = strings.TrimSpace(v)
			} else if v, ok := strings.CutPrefix(l, "PPid:"); ok {
				ppid = strings.TrimSpace(v)
			}
		}
		if name == "nvim" && ppid == self {
			n++
		}
	}
	return n
}
