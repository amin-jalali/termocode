package ai

import (
	"context"
	"reflect"
	"testing"
)

// OMX-25: small slips in a model's tool arguments are repaired; cut
// (truncated) arguments never are: a half file must not be written.
func TestRepairToolArgs(t *testing.T) {
	for in, want := range map[string]map[string]any{
		`{"path":"a.go",}`:                  {"path": "a.go"},
		"```json\n{\"path\":\"a.go\"}\n```": {"path": "a.go"},
		`"{\"path\":\"a.go\"}"`:             {"path": "a.go"},
		"\ufeff {\"path\":\"a.go\"} ":       {"path": "a.go"},
		"{\"text\":\"line 1\nline 2\tx\"}":  {"text": "line 1\nline 2\tx"},
		`{"list":[1,2,],"o":{"a":"x, }"},}`: {"list": []any{1.0, 2.0}, "o": map[string]any{"a": "x, }"}},
		`{"s":"a \"quoted\", }"}`:           {"s": `a "quoted", }`},
	} {
		got, ok := RepairToolArgs(in)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("RepairToolArgs(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{
		`{"command":"go te`,         // cut in a string
		`{"path":"a.go","content":`, // cut after a key
		`{"a":1`,                    // a missing brace is a cut stream
		`[1,2]`,                     // not an object
		`not json`,
		`"just a string"`,
		``,
	} {
		if got, ok := RepairToolArgs(in); ok {
			t.Errorf("RepairToolArgs(%q) repaired a cut or wrong value: %v", in, got)
		}
	}
}

func TestOpenAIRepairsToolArgs(t *testing.T) {
	srv := sseServer(t, nil,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{\"path\":\"a.go\",}"}}]}}]}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOllama, BaseURL: srv.URL})
	_, last := Collect(p.Stream(context.Background(), Request{}))
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].BadArgs || last.ToolCalls[0].Args["path"] != "a.go" {
		t.Fatalf("not repaired: %+v", last.ToolCalls)
	}
}

func TestAnthropicRepairsToolArgs(t *testing.T) {
	srv := sseServer(t, nil,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu","name":"read_file","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\",}"}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"})
	_, last := Collect(p.Stream(context.Background(), Request{}))
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].BadArgs || last.ToolCalls[0].Args["path"] != "a.go" {
		t.Fatalf("not repaired: %+v", last.ToolCalls)
	}
}
