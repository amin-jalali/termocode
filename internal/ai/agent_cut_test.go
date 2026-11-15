package ai

import (
	"context"
	"strings"
	"testing"
)

// cutScripted is scripted whose first reply hit the output limit.
type cutScripted struct{ scripted }

func (s *cutScripted) Stream(ctx context.Context, req Request) <-chan Chunk {
	first := s.calls == 0
	in := s.scripted.Stream(ctx, req)
	out := make(chan Chunk, 4)
	go func() {
		defer close(out)
		for c := range in {
			if c.Done && first {
				c.Cut = true
			}
			out <- c
		}
	}()
	return out
}

// Spec (lean request): an answer that hit the output token limit may
// end in a cut tool call (a long write_file). None of its calls run;
// each gets a note to send it again whole, and the turn goes on.
func TestAgentCutAnswerRunsNoCalls(t *testing.T) {
	ran := 0
	tool := Tool{Name: "write_file", Run: func(context.Context, map[string]any) (string, error) { ran++; return "written", nil }}
	p := &cutScripted{scripted{tools: true, replies: []Chunk{
		{ToolCalls: []ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": "half"}}}},
		{Delta: "sent again in parts"},
	}}}
	a := &Agent{Provider: p, Tools: []Tool{tool}, Native: true, AutoApprove: map[string]bool{}}
	_, added := collectEvents(a, []Message{{Role: RoleSystem, Content: "s"}, {Role: RoleUser, Content: "write it"}})
	if ran != 0 {
		t.Fatalf("a call of a cut answer ran %d times", ran)
	}
	var note string
	for _, m := range added {
		if m.Role == RoleTool && m.ToolCallID == "c1" {
			note = m.Content
		}
	}
	if !strings.Contains(note, "output token limit") || !strings.Contains(note, "not run") {
		t.Errorf("the cut call's result: %q", note)
	}
	if p.calls != 2 {
		t.Errorf("requests %d, want 2 (the turn goes on)", p.calls)
	}
}

// Spec: the providers say when an answer hit the output limit
// (Anthropic stop_reason max_tokens, OpenAI finish_reason length).
func TestProvidersMarkCutAnswers(t *testing.T) {
	ant := sseServer(t, nil,
		`{"type":"message_start","message":{"model":"claude-sonnet-5-5"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":15}}`,
		`{"type":"message_stop"}`)
	defer ant.Close()
	_, last := Collect(NewProvider(Config{Kind: KindAnthropic, BaseURL: ant.URL, APIKey: "k"}).Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if !last.Cut {
		t.Error("anthropic max_tokens not marked cut")
	}
	oai := sseServer(t, nil,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"Hi"}}]}`,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
		`[DONE]`)
	defer oai.Close()
	_, last = Collect(NewProvider(Config{Kind: KindOpenAI, BaseURL: oai.URL, APIKey: "k", Model: "m"}).Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if !last.Cut {
		t.Error("openai finish_reason length not marked cut")
	}
	ok := sseServer(t, nil,
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":"stop"}]}`,
		`[DONE]`)
	defer ok.Close()
	_, last = Collect(NewProvider(Config{Kind: KindOpenAI, BaseURL: ok.URL, APIKey: "k", Model: "m"}).Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if last.Cut {
		t.Error("a normal stop marked cut")
	}
}
