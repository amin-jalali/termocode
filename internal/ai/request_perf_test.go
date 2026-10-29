package ai

import (
	"strconv"
	"strings"
	"testing"
)

// perfSpecs are n tool specs shaped like the chat's tools.
func perfSpecs(n int) []ToolSpec {
	var ts []Tool
	for i := 0; i < n; i++ {
		ts = append(ts, Tool{Name: "tool_" + strconv.Itoa(i),
			Description: "Does thing " + strconv.Itoa(i) + " in the IDE. " + strings.Repeat("More words about it. ", 6),
			Params: map[string]any{
				"path":    map[string]any{"type": "string", "description": "file path, relative to the workspace root"},
				"op":      map[string]any{"type": "string", "enum": []string{"list", "add", "remove"}, "description": "what to do"},
				"wait_ms": map[string]any{"type": "integer", "description": "wait up to this long"},
			}, Required: []string{"path"}})
	}
	specs := make([]ToolSpec, len(ts))
	for i, t := range ts {
		specs[i] = t.Spec()
	}
	return specs
}

// perfRequest is one step's request: a 2 000-message history and 150
// tools.
func perfRequest() Request {
	msgs := []Message{{Role: RoleSystem, Content: strings.Repeat("You are termocode's assistant. ", 40)}}
	for i := 0; i < 1000; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: "Question " + strconv.Itoa(i)},
			Message{Role: RoleAssistant, Content: strings.Repeat("Here is the change:\n```go\nfunc f() int { return 42 }\n```\n", 6)})
	}
	return Request{Messages: msgs, Tools: perfSpecs(150)}
}

// A step after the first reuses the turn's specs (Agent.Run makes them
// once per turn).
func BenchmarkOpenAIRequestBody(b *testing.B) {
	req := perfRequest()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := marshalBody(buildOpenAIBody(req, "m")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnthropicRequestBody(b *testing.B) {
	req := perfRequest()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := marshalBody(buildAnthropicBody(req, "m", false)); err != nil {
			b.Fatal(err)
		}
	}
}
