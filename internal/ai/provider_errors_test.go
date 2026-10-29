package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deadURL returns the address of a server that is already closed, so a
// request to it fails to connect.
func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func TestProvidersNameAndTools(t *testing.T) {
	for kind, tools := range map[string]bool{KindOpenAI: true, KindOllama: true, KindAnthropic: true, KindClaudeOAuth: true, "": false} {
		p := NewProvider(Config{Kind: kind})
		want := kind
		if kind == "" {
			want = "none"
		}
		if p.Name() != want || p.SupportsTools() != tools {
			t.Errorf("%q: name %q tools %v", kind, p.Name(), p.SupportsTools())
		}
	}
}

func TestProvidersFailClearlyOnBadServer(t *testing.T) {
	ctx := context.Background()
	req := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	cases := map[string]Config{
		"openai":    {Kind: KindOpenAI, APIKey: "sk-x-1234567890"},
		"anthropic": {Kind: KindAnthropic, APIKey: "sk-ant-x-1234567890"},
	}
	for name, cfg := range cases {
		t.Run(name+"/no connection", func(t *testing.T) {
			cfg.BaseURL = deadURL(t)
			_, last := Collect(NewProvider(cfg).Stream(ctx, req))
			if last.Err == nil || strings.Contains(last.Err.Error(), cfg.APIKey) {
				t.Errorf("err = %v", last.Err)
			}
		})
		t.Run(name+"/bad url", func(t *testing.T) {
			cfg.BaseURL = "http://bad host"
			if _, last := Collect(NewProvider(cfg).Stream(ctx, req)); last.Err == nil {
				t.Error("a URL with a space must fail")
			}
		})
		t.Run(name+"/http 529", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(529)
				_, _ = w.Write([]byte(`{"error":{"type":"overloaded_error","message":"Overloaded"}}`))
			}))
			defer srv.Close()
			cfg.BaseURL = srv.URL
			_, last := Collect(NewProvider(cfg).Stream(ctx, req))
			if last.Err == nil || !strings.Contains(last.Err.Error(), "529") || !strings.Contains(last.Err.Error(), "Overloaded") {
				t.Errorf("err = %v", last.Err)
			}
		})
		t.Run(name+"/line too long", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("data: " + strings.Repeat("x", 5*1024*1024) + "\n\n"))
			}))
			defer srv.Close()
			cfg.BaseURL = srv.URL
			if _, last := Collect(NewProvider(cfg).Stream(ctx, req)); last.Err == nil || !strings.Contains(last.Err.Error(), "too long") {
				t.Errorf("err = %v", last.Err)
			}
		})
	}
}

func TestOpenAIUnconfiguredAndErrorEvent(t *testing.T) {
	if _, last := Collect(NewProvider(Config{Kind: KindOpenAI}).Stream(context.Background(), Request{})); last.Err != ErrNotConfigured {
		t.Errorf("no key: %v", last.Err)
	}
	key := "sk-secret-abcdefghijklmnop"
	RegisterSecret(key)
	srv := sseServer(t, nil, `{"error":{"message":"quota gone for `+key+`"}}`)
	defer srv.Close()
	_, last := Collect(NewProvider(Config{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: key}).Stream(context.Background(), Request{}))
	if last.Err == nil || !strings.Contains(last.Err.Error(), "quota gone") || strings.Contains(last.Err.Error(), key) {
		t.Errorf("error event: %v", last.Err)
	}
}

func TestOpenAIBodySendsTemperatureAndMaxTokens(t *testing.T) {
	temp := 0.3
	body := buildOpenAIBody(Request{Temperature: &temp, MaxTokens: 99}, "")
	if body["temperature"] != 0.3 || body["max_tokens"] != 99 {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["model"]; ok {
		t.Error("no model must leave model out")
	}
	if _, ok := buildOpenAIBody(Request{}, "m")["temperature"]; ok {
		t.Error("temperature sent without being set")
	}
}

func TestAnthropicUnconfiguredAndSignIn(t *testing.T) {
	ctx := context.Background()
	if _, last := Collect(NewProvider(Config{Kind: KindAnthropic}).Stream(ctx, Request{})); last.Err != ErrNotConfigured {
		t.Errorf("no key: %v", last.Err)
	}
	empty := Config{Kind: KindClaudeOAuth, BaseURL: "http://127.0.0.1:1", Token: func(context.Context) (string, error) { return "", nil }}
	if _, last := Collect(NewProvider(empty).Stream(ctx, Request{})); last.Err == nil || !strings.Contains(last.Err.Error(), "not signed in") {
		t.Errorf("empty token: %v", last.Err)
	}
	secret := "refresh-secret-token-abcdef"
	RegisterSecret(secret)
	failing := Config{Kind: KindClaudeOAuth, BaseURL: "http://127.0.0.1:1", Token: func(context.Context) (string, error) {
		return "", errors.New("refresh failed for " + secret)
	}}
	_, last := Collect(NewProvider(failing).Stream(ctx, Request{}))
	if last.Err == nil || !strings.Contains(last.Err.Error(), "refresh failed") || strings.Contains(last.Err.Error(), secret) {
		t.Errorf("token error: %v", last.Err)
	}
}

func TestAnthropicBodyReplaysAssistantTurns(t *testing.T) {
	blocks := json.RawMessage(`[{"type":"thinking","thinking":"t","signature":"s"}]`)
	body := buildAnthropicBody(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "q"},
			{Role: RoleAssistant, Content: "plain answer"},
			{Role: RoleAssistant, Blocks: blocks, Content: "ignored"},
			{Role: RoleAssistant, Content: "let me look", ToolCalls: []ToolCall{{ID: "t1", Name: "read_file"}}},
			{Role: RoleTool, ToolCallID: "t1", Content: "file"},
		},
		Tools: []ToolSpec{{Name: "read_file", Description: "Read", Schema: map[string]any{"type": "object"}}},
	}, "m", false)
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{
		`{"content":"plain answer","role":"assistant"}`,
		`{"content":[{"type":"thinking","thinking":"t","signature":"s"}],"role":"assistant"}`,
		`{"text":"let me look","type":"text"},{"id":"t1","input":{},"name":"read_file","type":"tool_use"}`,
		`"tools":[{"description":"Read","input_schema":{"type":"object"},"name":"read_file"}]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body lacks %s\n%s", want, s)
		}
	}
	if _, ok := body["system"]; ok {
		t.Error("no system text must leave system out")
	}
}

func TestAnthropicStreamOddEvents(t *testing.T) {
	srv := sseServer(t, nil,
		``,
		`[DONE]`,
		`{broken`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hi"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque"}}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t","name":"list_dir"}}`)
	defer srv.Close()
	text, last := Collect(NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"}).Stream(context.Background(), Request{}))
	// The stream ends without message_stop: what came is still returned.
	if text != "Hi!" || last.Err != nil {
		t.Fatalf("text %q err %v", text, last.Err)
	}
	if !strings.Contains(string(last.Blocks), `{"data":"opaque","type":"redacted_thinking"}`) {
		t.Errorf("redacted thinking not kept for replay: %s", last.Blocks)
	}
	if len(last.ToolCalls) != 1 || last.ToolCalls[0].Args == nil || last.ToolCalls[0].BadArgs {
		t.Errorf("tool call with no input: %+v", last.ToolCalls)
	}
}

func TestAnthropicErrorEventWithoutMessage(t *testing.T) {
	srv := sseServer(t, nil, `{"type":"error","error":{"type":"api_error"}}`)
	defer srv.Close()
	_, last := Collect(NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"}).Stream(context.Background(), Request{}))
	if last.Err == nil || last.Err.Error() != "Claude error: api_error" {
		t.Errorf("err = %v", last.Err)
	}
}

func TestAnthropicStreamCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"a\"}}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithCancel(context.Background())
	ch := NewProvider(Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "k-12345678"}).Stream(ctx, Request{})
	if c := <-ch; c.Delta != "a" {
		t.Fatalf("first chunk %+v", c)
	}
	cancel()
	if _, last := Collect(ch); !errors.Is(last.Err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", last.Err)
	}
}
