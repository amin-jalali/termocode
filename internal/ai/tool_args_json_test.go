package ai

import (
	"context"
	"strings"
	"testing"
)

// A tool call whose arguments are cut or broken JSON (a dropped stream, a
// model slip) must not run with no arguments: run_command would run an
// empty command and tell the model "[no output, exit 0]".

func TestOpenAIMarksBrokenToolArgs(t *testing.T) {
	srv := sseServer(t, nil,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"run_command","arguments":"{\"command\":\"go te"}}]}}]}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOllama, BaseURL: srv.URL})
	_, last := Collect(p.Stream(context.Background(), Request{}))
	if len(last.ToolCalls) != 1 || !last.ToolCalls[0].BadArgs {
		t.Fatalf("broken arguments not marked: %+v", last.ToolCalls)
	}
}

func TestAnthropicMarksBrokenToolArgs(t *testing.T) {
	srv := sseServer(t, nil,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu","name":"run_command","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"go te"}}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"})
	_, last := Collect(p.Stream(context.Background(), Request{}))
	if len(last.ToolCalls) != 1 || !last.ToolCalls[0].BadArgs {
		t.Fatalf("broken arguments not marked: %+v", last.ToolCalls)
	}
}

func TestAgentRefusesCallWithBrokenArgs(t *testing.T) {
	ran := false
	tool := Tool{Name: "run_command", Mutating: true, Class: ClassRun,
		Run: func(context.Context, map[string]any) (string, error) { ran = true; return "[no output, exit 0]", nil }}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "c1", Name: "run_command", BadArgs: true}}}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool}, AutoApprove: map[string]bool{ClassRun: true}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "test"}})
	if ran {
		t.Fatal("the tool ran with broken arguments")
	}
	var got Event
	for _, e := range evs {
		if e.Kind == EventToolResult {
			got = e
		}
	}
	if got.Gate != GateInvalid || !strings.Contains(got.Detail, "not valid JSON") {
		t.Errorf("result event = %+v", got)
	}
	// The model is told, so it can send the call again.
	if len(p.seen) < 2 || !strings.Contains(lastToolText(p.seen[1]), "not valid JSON") {
		t.Errorf("model not told: %+v", p.seen)
	}
}

func lastToolText(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleTool {
			return msgs[i].Content
		}
	}
	return ""
}
