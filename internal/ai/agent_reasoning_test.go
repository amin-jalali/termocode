package ai

import (
	"context"
	"sync"
	"testing"
)

// recorder is a fake Provider that keeps every request and answers
// with the next scripted chunk ("ok" when none is left).
type recorder struct {
	mu      sync.Mutex
	name    string
	replies []Chunk
	reqs    []Request
}

func (r *recorder) Name() string        { return r.name }
func (r *recorder) Configured() bool    { return true }
func (r *recorder) SupportsTools() bool { return true }
func (r *recorder) Stream(_ context.Context, req Request) <-chan Chunk {
	r.mu.Lock()
	c := Chunk{Delta: "ok"}
	if n := len(r.reqs); n < len(r.replies) {
		c = r.replies[n]
	}
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	ch := make(chan Chunk, 2)
	if c.Delta != "" {
		ch <- Chunk{Delta: c.Delta}
	}
	c.Delta, c.Done = "", true
	ch <- c
	close(ch)
	return ch
}

func (r *recorder) requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.reqs...)
}

func TestAgentSendsReasoning(t *testing.T) {
	p := &recorder{}
	a := &Agent{Provider: p, Native: true, Reasoning: ReasoningHigh}
	collectEvents(a, []Message{{Role: RoleUser, Content: "q"}})
	if rs := p.requests(); len(rs) != 1 || rs[0].Reasoning != ReasoningHigh {
		t.Fatalf("requests: %+v", rs)
	}
}
