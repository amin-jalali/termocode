package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// scriptProvider answers every request with the next scripted tool call
// (then a plain "done").
type scriptProvider struct {
	mu    sync.Mutex
	calls []ai.ToolCall
}

func (p *scriptProvider) Name() string        { return "script" }
func (p *scriptProvider) Configured() bool    { return true }
func (p *scriptProvider) SupportsTools() bool { return true }
func (p *scriptProvider) Stream(_ context.Context, _ ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan ai.Chunk, 1)
	if len(p.calls) > 0 {
		ch <- ai.Chunk{Done: true, ToolCalls: p.calls[:1]}
		p.calls = p.calls[1:]
	} else {
		ch <- ai.Chunk{Done: true, Delta: "done"}
	}
	close(ch)
	return ch
}

// Spec (fix 6): in the in-app chat, a declined http_send leaves nothing in
// httpAICache, and a request cached for an earlier arguments map is never
// used by a new call whose map has the same address (the cache is keyed by
// the map's address).
func TestChatHTTPSendDeclinedLeavesNoStaleRequest(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&m)
		return out, err
	}
	var cards []string
	agent := func(args map[string]any) *ai.Agent {
		return &ai.Agent{
			Provider: &scriptProvider{calls: []ai.ToolCall{{ID: "1", Name: "http_send", Args: args}}},
			Tools:    m.aiChatTools(root, call),
			Native:   true,
			Confirm: func(_ context.Context, _ ai.Tool, human string) bool {
				cards = append(cards, human)
				return false // decline
			},
		}
	}
	run := func(args map[string]any) {
		agent(args).Run(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: "go"}}, func(ai.Event) {})
	}

	first := map[string]any{"method": "POST", "url": "http://example.invalid/first", "body": "{}"}
	run(first)
	if len(cards) != 1 || !strings.Contains(cards[0], "/first") {
		t.Fatalf("cards = %q", cards)
	}
	if _, ok := httpAICache.Load(httpAIKey(first)); ok {
		t.Fatal("a declined call left its request in the cache")
	}

	// A new call whose map "lands at the same address": simulate it by
	// planting the old request under the new map's key.
	second := map[string]any{"method": "POST", "url": "http://example.invalid/second", "body": "{}"}
	httpAICache.Store(httpAIKey(second), httpAIBuild(root, first))
	run(second)
	if len(cards) != 2 || !strings.Contains(cards[1], "/second") || strings.Contains(cards[1], "/first") {
		t.Fatalf("the second call showed the first request: %q", cards)
	}
	if _, ok := httpAICache.Load(httpAIKey(second)); ok {
		t.Fatal("a declined call left its request in the cache")
	}
}
