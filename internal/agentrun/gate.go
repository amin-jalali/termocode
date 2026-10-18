package agentrun

import (
	"context"
	"sync"
)

// Gate lets one node at a time show a card that waits for the user (a
// tool confirm or a file review). Unlike a mutex, a wait for it ends when
// the waiting node's context ends, so a stopped run never hangs behind
// another node's open card. Waiters are served in the order they came
// (Go queues blocked channel senders first in, first out). The zero
// Gate is ready to use.
type Gate struct {
	once sync.Once
	ch   chan struct{}
}

func (g *Gate) c() chan struct{} {
	g.once.Do(func() { g.ch = make(chan struct{}, 1) })
	return g.ch
}

// Acquire waits for the gate; it returns ctx's error when ctx ends first
// (the gate is then not held).
func (g *Gate) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.c() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire takes the gate when it is free.
func (g *Gate) TryAcquire() bool {
	select {
	case g.c() <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release frees the gate (a no-op when it is free).
func (g *Gate) Release() {
	select {
	case <-g.c():
	default:
	}
}

type reviewKey struct{}

type parallelKey struct{}

// ReviewGate returns the review gate of the run ctx belongs to (nil
// outside a run or when the run has none).
func ReviewGate(ctx context.Context) *Gate {
	g, _ := ctx.Value(reviewKey{}).(*Gate)
	return g
}

// Parallel reports whether ctx belongs to a run whose nodes may run at
// the same time (Options.Parallel > 1).
func Parallel(ctx context.Context) bool {
	p, _ := ctx.Value(parallelKey{}).(bool)
	return p
}
