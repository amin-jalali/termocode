package ai

import (
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
