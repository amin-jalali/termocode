package agentrun

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// watchdog stops a step's agent when the model has been silent for the
// stall limit (OMX-23). A running tool call is no silence: tools keep
// their own time limits, and a card waits for the user.
type watchdog struct {
	limit time.Duration
	stall context.CancelFunc
	busy  atomic.Int32
	fired atomic.Bool
	done  chan struct{}
	once  sync.Once

	mu   sync.Mutex
	last time.Time
}

func startWatchdog(limit time.Duration, stall context.CancelFunc) *watchdog {
	d := &watchdog{limit: limit, stall: stall, done: make(chan struct{}), last: time.Now()}
	crash.Go("the agent step stall watchdog", d.watch)
	return d
}

// touch records activity; tool counts the tool calls that run (+1 at
// the start, -1 at the end).
func (d *watchdog) touch(tool int32) {
	d.busy.Add(tool)
	d.mu.Lock()
	d.last = time.Now()
	d.mu.Unlock()
}

func (d *watchdog) stop() { d.once.Do(func() { close(d.done) }) }

func (d *watchdog) watch() {
	t := time.NewTimer(d.limit)
	defer t.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-t.C:
		}
		d.mu.Lock()
		quiet := time.Since(d.last)
		d.mu.Unlock()
		if d.busy.Load() > 0 {
			quiet = 0
		}
		if quiet >= d.limit {
			d.fired.Store(true)
			d.stall()
			return
		}
		t.Reset(d.limit - quiet)
	}
}

// StallText says how long a stalled step was silent.
func StallText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	if d >= time.Second {
		return d.Round(time.Second).String()
	}
	return d.String()
}

// EmptyPrompt asks a step once more when its answer was empty.
const EmptyPrompt = "Your last answer was empty. Give your final answer now: in a few lines, say what you did and what you found."
