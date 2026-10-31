package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextTooLongKnowsTheProviderErrors(t *testing.T) {
	for _, msg := range []string{
		"prompt is too long: 210000 tokens > 200000 maximum",
		"This model's maximum context length is 128000 tokens",
		"context_length_exceeded",
		"input is too long for requested model",
		"Request too large: too many tokens",
	} {
		if !ContextTooLong(statusError{code: 400, msg: "HTTP 400: " + msg}) {
			t.Errorf("%q: not seen as too long", msg)
		}
	}
	if !ContextTooLong(statusError{code: 413, msg: "HTTP 413: payload"}) {
		t.Error("413 is too long")
	}
	for _, err := range []error{nil, errors.New("prompt is too long"), statusError{code: 400, msg: "HTTP 400: bad tool schema"}, statusError{code: 500, msg: "HTTP 500: context length"}} {
		if ContextTooLong(err) {
			t.Errorf("%v: seen as too long", err)
		}
	}
}

func TestEstimateTokensCountsEveryPart(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: strings.Repeat("a", 400)},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "read_file", Args: map[string]any{"path": strings.Repeat("b", 400)}}}},
		{Role: RoleTool, Content: strings.Repeat("c", 400)},
	}
	if n := EstimateTokens(msgs); n < 300 || n > 340 {
		t.Fatalf("estimate %d, want about 300", n)
	}
}

func TestCompactTranscriptRedactsAndCaps(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "my key is sk-" + "ant-test0123456789abcdefghij"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{{Name: "read_file", Args: map[string]any{"path": "a.go"}}}},
		{Role: RoleTool, ToolName: "read_file", Content: strings.Repeat("x", 5000)},
	}
	got := CompactTranscript(msgs)
	if strings.Contains(got, "0123456789abcdefghij") {
		t.Fatalf("key reached the summary request: %s", got)
	}
	if !strings.Contains(got, "User: my key") || !strings.Contains(got, "read_file") || len(got) > 4000 {
		t.Fatalf("transcript:\n%s", got)
	}
}

func TestCompactedMessageWrapsTheSummary(t *testing.T) {
	m := CompactedMessage("We fixed a.go.")
	if m.Role != RoleUser || !strings.Contains(m.Content, "We fixed a.go.") || !strings.Contains(m.Content, "summary") {
		t.Fatalf("%+v", m)
	}
}

func TestAgentCompactsOnceWhenTheContextIsTooLong(t *testing.T) {
	tooLong := statusError{code: 400, msg: "HTTP 400: prompt is too long"}
	p := &scripted{replies: []Chunk{{Err: tooLong}, {Delta: "fine now"}}}
	var older []Message
	a := &Agent{Provider: p, Compact: func(_ context.Context, msgs []Message) (string, error) {
		older = msgs
		return "We read a.go.", nil
	}}
	hist := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "read a.go"},
		{Role: RoleAssistant, Content: "it is long"},
		{Role: RoleUser, Content: "now fix it"},
	}
	evs, _ := collectEvents(a, hist)
	if len(older) != 2 || older[0].Content != "read a.go" {
		t.Fatalf("summarised %+v", older)
	}
	if p.calls != 2 {
		t.Fatalf("calls %d", p.calls)
	}
	sent := p.seen[1]
	if sent[0].Content != "sys" || !strings.Contains(sent[1].Content, "We read a.go.") || sent[len(sent)-1].Content != "now fix it" {
		t.Fatalf("second request %+v", sent)
	}
	var compacted, errs int
	for _, e := range evs {
		switch e.Kind {
		case EventCompacted:
			compacted++
			if e.Detail != "We read a.go." || e.Attempt != 2 {
				t.Errorf("event %+v", e)
			}
		case EventError:
			errs++
		}
	}
	if compacted != 1 || errs != 0 {
		t.Fatalf("compacted %d errors %d", compacted, errs)
	}
}

func TestAgentCompactsOnlyOnce(t *testing.T) {
	tooLong := statusError{code: 400, msg: "HTTP 400: prompt is too long"}
	p := &scripted{replies: []Chunk{{Err: tooLong}, {Err: tooLong}, {Delta: "never"}}}
	n := 0
	a := &Agent{Provider: p, Compact: func(context.Context, []Message) (string, error) { n++; return "s", nil }}
	evs, _ := collectEvents(a, []Message{{Role: RoleSystem}, {Role: RoleUser, Content: "a"}, {Role: RoleAssistant, Content: "b"}, {Role: RoleUser, Content: "c"}})
	if n != 1 || p.calls != 2 || evs[len(evs)-1].Kind != EventError {
		t.Fatalf("compactions %d calls %d last %+v", n, p.calls, evs[len(evs)-1])
	}
}

func TestAgentDoesNotCompactOtherErrorsOrAFirstTurn(t *testing.T) {
	n := 0
	compact := func(context.Context, []Message) (string, error) { n++; return "s", nil }
	p := &scripted{replies: []Chunk{{Err: statusError{code: 401, msg: "HTTP 401: bad key"}}}}
	collectEvents(&Agent{Provider: p, Compact: compact}, []Message{{Role: RoleSystem}, {Role: RoleUser, Content: "a"}, {Role: RoleAssistant, Content: "b"}, {Role: RoleUser, Content: "c"}})
	p = &scripted{replies: []Chunk{{Err: statusError{code: 400, msg: "HTTP 400: prompt is too long"}}}}
	collectEvents(&Agent{Provider: p, Compact: compact}, []Message{{Role: RoleSystem}, {Role: RoleUser, Content: "a"}})
	if n != 0 {
		t.Fatalf("compacted %d times", n)
	}
}
