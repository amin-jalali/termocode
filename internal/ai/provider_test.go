package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(t *testing.T, check func(r *http.Request, body map[string]any), events ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		if check != nil {
			check(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
}

func TestOpenAIStreamText(t *testing.T) {
	var gotAuth, gotPath string
	srv := sseServer(t, func(r *http.Request, body map[string]any) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		if body["model"] != "m1" || body["stream"] != true {
			t.Errorf("body: %v", body)
		}
	},
		`{"choices":[{"delta":{"content":"Hel"}}]}`,
		`not json`,
		`{"choices":[{"delta":{"content":"lo"}}]}`,
		`[DONE]`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOpenAI, BaseURL: srv.URL + "/v1/", APIKey: "sk-test-1234567890abcdef", Model: "m1"})
	text, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if text != "Hello" || last.Err != nil {
		t.Fatalf("text=%q err=%v", text, last.Err)
	}
	if gotAuth != "Bearer sk-test-1234567890abcdef" || gotPath != "/v1/chat/completions" {
		t.Errorf("auth=%q path=%q", gotAuth, gotPath)
	}
}

func TestOpenAIToolCalls(t *testing.T) {
	srv := sseServer(t, func(r *http.Request, body map[string]any) {
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Errorf("tools not sent: %v", body["tools"])
		}
	},
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.go\"}"}}]}}]}`,
		`[DONE]`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOllama, BaseURL: srv.URL})
	_, last := Collect(p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "read"}},
		Tools:    []ToolSpec{{Name: "read_file", Schema: map[string]any{"type": "object"}}},
	}))
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != "read_file" || last.ToolCalls[0].Args["path"] != "a.go" || last.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls: %+v", last.ToolCalls)
	}
}

func TestOpenAIHTTPErrorIsRedacted(t *testing.T) {
	key := "sk-secret-abcdefghijklmnop"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprintf(w, `{"error":{"message":"bad key %s"}}`, key)
	}))
	defer srv.Close()
	p := NewProvider(Config{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: key})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}))
	if last.Err == nil || !strings.Contains(last.Err.Error(), "HTTP 401") || strings.Contains(last.Err.Error(), key) {
		t.Fatalf("err=%v", last.Err)
	}
}

func TestOpenAIBodyToolRoundTrip(t *testing.T) {
	body := buildOpenAIBody(Request{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "x", Args: map[string]any{"a": 1}}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "ok"},
	}}, "m")
	b, _ := json.Marshal(body)
	s := string(b)
	if !strings.Contains(s, `"tool_call_id":"c1"`) || !strings.Contains(s, `"arguments":"{\"a\":1}"`) || !strings.Contains(s, `"content":null`) {
		t.Fatalf("body: %s", s)
	}
}

func TestAnthropicStreamAPIKey(t *testing.T) {
	srv := sseServer(t, func(r *http.Request, body map[string]any) {
		if r.Header.Get("x-api-key") != "sk-ant-test-key-123456" || r.Header.Get("anthropic-version") == "" || r.URL.Path != "/v1/messages" {
			t.Errorf("headers: %v path %s", r.Header, r.URL.Path)
		}
		if _, ok := body["temperature"]; ok {
			t.Error("temperature must not be sent")
		}
		sys, _ := body["system"].([]any)
		if len(sys) != 1 || body["model"] != ModelClaudeOpus {
			t.Errorf("system/model: %v %v", body["system"], body["model"])
		}
	},
		`{"type":"message_start"}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hi "}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"there"}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	temp := 0.2
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "sk-ant-test-key-123456"})
	text, last := Collect(p.Stream(context.Background(), Request{Temperature: &temp, Messages: []Message{
		{Role: RoleSystem, Content: "be brief"}, {Role: RoleUser, Content: "hi"},
	}}))
	if text != "Hi there" || last.Err != nil {
		t.Fatalf("text=%q err=%v", text, last.Err)
	}
	if !strings.Contains(string(last.Blocks), `"signature":"sig"`) {
		t.Errorf("thinking block not kept for replay: %s", last.Blocks)
	}
}

func TestAnthropicToolUseAndOAuth(t *testing.T) {
	srv := sseServer(t, func(r *http.Request, body map[string]any) {
		if r.Header.Get("Authorization") != "Bearer tok-abcdefgh" || r.Header.Get("anthropic-beta") != anthropicOAuthBeta || r.Header.Get("x-api-key") != "" {
			t.Errorf("oauth headers: %v", r.Header)
		}
		sys, _ := body["system"].([]any)
		first, _ := sys[0].(map[string]any)
		if first["text"] != claudeCodeIdentity {
			t.Errorf("identity block missing: %v", sys)
		}
		msgs, _ := body["messages"].([]any)
		last, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := last["content"].([]any)
		if len(content) != 2 {
			t.Errorf("tool results must share one user turn: %v", last)
		}
	},
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"search","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"foo\"}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindClaudeOAuth, BaseURL: srv.URL, Token: func(context.Context) (string, error) { return "tok-abcdefgh", nil }})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "x"}, {ID: "b", Name: "y"}}},
		{Role: RoleTool, ToolCallID: "a", Content: "1"},
		{Role: RoleTool, ToolCallID: "b", Content: "2"},
	}}))
	if last.Err != nil || len(last.ToolCalls) != 1 || last.ToolCalls[0].Args["query"] != "foo" || last.ToolCalls[0].ID != "tu_1" {
		t.Fatalf("err=%v calls=%+v", last.Err, last.ToolCalls)
	}
}

func TestAnthropicRefusalAndError(t *testing.T) {
	srv := sseServer(t, nil,
		`{"type":"message_delta","delta":{"stop_reason":"refusal"}}`,
		`{"type":"message_stop"}`)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"})
	if _, last := Collect(p.Stream(context.Background(), Request{})); last.Err == nil {
		t.Error("refusal should surface an error")
	}
	srv2 := sseServer(t, nil, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	defer srv2.Close()
	p2 := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv2.URL, APIKey: "k-12345678"})
	if _, last := Collect(p2.Stream(context.Background(), Request{})); last.Err == nil || !strings.Contains(last.Err.Error(), "Overloaded") {
		t.Errorf("error event: %v", last.Err)
	}
}

func TestUnconfigured(t *testing.T) {
	p := NewProvider(Config{})
	if p.Configured() {
		t.Fatal("empty config must be unconfigured")
	}
	if _, last := Collect(p.Stream(context.Background(), Request{})); last.Err != ErrNotConfigured {
		t.Fatalf("err=%v", last.Err)
	}
	if NewProvider(Config{Kind: KindAnthropic}).Configured() {
		t.Error("anthropic without key must be unconfigured")
	}
	if !NewProvider(Config{Kind: KindOllama}).Configured() {
		t.Error("ollama needs no key")
	}
}

func TestStreamCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithCancel(context.Background())
	ch := NewProvider(Config{Kind: KindOllama, BaseURL: srv.URL}).Stream(ctx, Request{})
	if c := <-ch; c.Delta != "a" {
		t.Fatalf("first chunk %+v", c)
	}
	cancel()
	_, last := Collect(ch)
	if last.Err == nil {
		t.Fatal("cancel should end the stream with an error")
	}
}
