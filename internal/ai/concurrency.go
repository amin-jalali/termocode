package ai

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Concurrency limits (OMX-26): config.json "ai_concurrency" caps how many
// requests run at once per provider kind ("anthropic": 2) or per model
// ("openai/gpt-5": 1), across the chat, agent runs and ghost text, so
// parallel steps do not cause a storm of 429 answers. A request over the
// limit waits (before anything is sent) until one ends or its turn is
// stopped. No entry: no limit.

// MaxConcurrency caps one ai_concurrency value.
const MaxConcurrency = 64

var limiter struct {
	mu   sync.Mutex
	sems map[string]chan struct{}
}

var limitWaiters atomic.Int32

// limitWaiting is how many requests wait for a slot now (tests).
func limitWaiting() int { return int(limitWaiters.Load()) }

// SetConcurrencyLimits replaces the limits (ConcurrencyLimits' result;
// nil: none). Requests running under the old limits keep their slots.
func SetConcurrencyLimits(limits map[string]int) {
	sems := map[string]chan struct{}{}
	for k, n := range limits {
		if n > 0 {
			sems[k] = make(chan struct{}, n)
		}
	}
	limiter.mu.Lock()
	limiter.sems = sems
	limiter.mu.Unlock()
}

// ConcurrencyLimit is the limit in use for key (a kind or
// "<kind>/<model>"; 0: none).
func ConcurrencyLimit(key string) int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return cap(limiter.sems[key])
}

// acquireLimit waits for a slot of "<kind>/<model>", then of kind (always
// in that order: no deadlock), and returns the function that frees both.
// It fails only when ctx ends first.
func acquireLimit(ctx context.Context, kind, model string) (func(), error) {
	limiter.mu.Lock()
	var chain []chan struct{}
	if c := limiter.sems[kind+"/"+model]; c != nil && model != "" {
		chain = append(chain, c)
	}
	if c := limiter.sems[kind]; c != nil {
		chain = append(chain, c)
	}
	limiter.mu.Unlock()
	var got []chan struct{}
	release := func() {
		for _, c := range got {
			<-c
		}
	}
	for _, c := range chain {
		select {
		case c <- struct{}{}:
		default:
			limitWaiters.Add(1)
			select {
			case c <- struct{}{}:
				limitWaiters.Add(-1)
			case <-ctx.Done():
				limitWaiters.Add(-1)
				release()
				return nil, ctx.Err()
			}
		}
		got = append(got, c)
	}
	return release, nil
}

// ConcurrencyLimits is the checked ai_concurrency: keys are a provider
// kind or "<kind>/<model>", values 1 to MaxConcurrency. Bad entries are
// left out and named in problems.
func (s Settings) ConcurrencyLimits() (map[string]int, []string) {
	out := map[string]int{}
	var problems []string
	keys := make([]string, 0, len(s.Concurrency))
	for k := range s.Concurrency {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := s.Concurrency[k]
		kind, model, hasModel := strings.Cut(strings.TrimSpace(k), "/")
		kind = strings.ToLower(kind)
		switch {
		case !knownKind(kind) || (hasModel && model == ""):
			problems = append(problems, fmt.Sprintf("config.json: ai_concurrency: %q is not a provider (%s) or <provider>/<model>", k, strings.Join(Kinds, ", ")))
		case n < 1 || n > MaxConcurrency:
			problems = append(problems, fmt.Sprintf("config.json: ai_concurrency: %q must be 1 to %d (it is %d)", k, MaxConcurrency, n))
		default:
			key := kind
			if hasModel {
				key += "/" + model
			}
			out[key] = n
		}
	}
	return out, problems
}

// streamLimited runs one request of p under the limits: the Done chunk
// is ctx's error when the turn stops while it waits.
func streamLimited(ctx context.Context, kind, model string, run func() Chunk) Chunk {
	release, err := acquireLimit(ctx, kind, model)
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	defer release()
	return run()
}
