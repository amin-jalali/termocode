package ai

import (
	"context"
	"sync"
)

// Tool progress (MCPX-19): a long tool (an MCP call that sends
// notifications/progress) reports short text while it runs; the agent
// emits it as EventToolProgress, so the chat shows what the call does.

// EventToolProgress: a running tool says what it does (Tool, Label,
// Detail). Its value is far from the iota list so new kinds there never
// clash with it.
const EventToolProgress EventKind = 100

type progressKey struct{}

type progressSink struct {
	mu   sync.Mutex
	emit func(string) // nil once the call ended
}

// ReportProgress tells the user what the running tool does now. It may
// be called from any goroutine; outside a call it does nothing.
func ReportProgress(ctx context.Context, text string) {
	s, _ := ctx.Value(progressKey{}).(*progressSink)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emit != nil {
		s.emit(text)
	}
}

// withProgress returns ctx with a sink that emits for t, and the func
// that ends it (later reports are dropped).
func withProgress(ctx context.Context, t Tool, emit func(Event)) (context.Context, func()) {
	s := &progressSink{emit: func(text string) {
		emit(Event{Kind: EventToolProgress, Tool: t.Name, Label: t.Label(), Detail: Redact(text)})
	}}
	return context.WithValue(ctx, progressKey{}, s), func() {
		s.mu.Lock()
		s.emit = nil
		s.mu.Unlock()
	}
}
