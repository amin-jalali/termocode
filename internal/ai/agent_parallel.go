package ai

import (
	"context"
	"fmt"
	"sync"
)

// maxParallelCalls caps how many read-only calls run at the same time.
const maxParallelCalls = 8

// runCalls runs the calls of one reply and returns their results in
// call order. A run of read-only calls (Tool.ReadOnly) runs at the same
// time; any other call runs alone, after the calls before it. The events
// of a parallel run are kept and emitted in call order from this
// goroutine, so emit is never called from two goroutines.
func (a *Agent) runCalls(ctx context.Context, calls []ToolCall, emit func(Event)) ([]string, [][]Image) {
	results := make([]string, len(calls))
	images := make([][]Image, len(calls))
	// one image sink per call: a vision model gets each call's images
	run := func(k int, emit func(Event)) {
		ictx, imgs := a.withImageSink(ctx)
		results[k] = a.runTool(ictx, calls[k], emit)
		images[k] = imgs()
	}
	for i := 0; i < len(calls); {
		j := i
		for j < len(calls) && a.readOnlyCall(calls[j]) {
			j++
		}
		if j-i < 2 {
			run(i, emit)
			i++
			continue
		}
		evs := make([][]Event, j-i)
		sem := make(chan struct{}, maxParallelCalls)
		var wg sync.WaitGroup
		for k := i; k < j; k++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(k int) {
				defer wg.Done()
				defer func() { <-sem }()
				keep := func(e Event) { evs[k-i] = append(evs[k-i], e) }
				defer func() {
					if r := recover(); r != nil {
						results[k] = fmt.Sprintf("error: %v", r)
					}
				}()
				run(k, keep)
			}(k)
		}
		wg.Wait()
		for _, es := range evs {
			for _, e := range es {
				emit(e)
			}
		}
		i = j
	}
	return results, images
}

// readOnlyCall reports whether call may run next to others: a known
// read-only tool with valid arguments.
func (a *Agent) readOnlyCall(call ToolCall) bool {
	if call.BadArgs {
		return false
	}
	t, ok := FindTool(a.Tools, call.Name)
	return ok && t.ReadOnly && !t.Mutating && t.Gate == nil && t.Class == ""
}
