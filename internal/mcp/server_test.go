package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/extproc"
)

type serverTestSource struct {
	list []Tool
	call func(context.Context, string, map[string]any) (CallResult, error)
}

func (s serverTestSource) List() []Tool { return s.list }
func (s serverTestSource) Call(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	return s.call(ctx, name, args)
}

func TestServerGolden(t *testing.T) {
	source := serverTestSource{
		list: []Tool{{Name: "read_file", Description: "Read a file", InputSchema: map[string]any{"type": "object"}}},
		call: func(_ context.Context, name string, args map[string]any) (CallResult, error) {
			return CallResult{Content: []Content{{Type: "text", Text: "file contents"}}}, nil
		},
	}
	for _, golden := range []string{"initialize", "tools_list", "tools_call"} {
		t.Run(golden, func(t *testing.T) {
			input := map[string]string{
				"initialize": `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
				"tools_list": `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
				"tools_call": `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"a.txt"}}}`,
			}[golden]
			got := runServerMessages(t, source, input)
			path := filepath.Join("testdata", "server", golden+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("exchange mismatch\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestServerErrors(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		source      ToolSource
		code        int
	}{
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"wat"}`, serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) { return CallResult{}, nil }}, -32601},
		{"bad params", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":7}}`, serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) { return CallResult{}, nil }}, -32602},
		{"unavailable", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil, -32001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runServerMessages(t, tc.source, tc.input)
			var response map[string]any
			if err := json.Unmarshal([]byte(got), &response); err != nil {
				t.Fatal(err)
			}
			if int(response["error"].(map[string]any)["code"].(float64)) != tc.code {
				t.Fatalf("unexpected response: %s", got)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		callErr error
		code    int
	}{
		{"declined", errors.New("declined by the user"), -32000},
		{"source unavailable", ErrToolSourceUnavailable, -32001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) { return CallResult{}, tc.callErr }}
			got := runServerMessages(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
			var response map[string]any
			_ = json.Unmarshal([]byte(got), &response)
			rpcErr, ok := response["error"].(map[string]any)
			if !ok || int(rpcErr["code"].(float64)) != tc.code {
				t.Fatalf("expected error code %d: %s", tc.code, got)
			}
		})
	}
}

func TestServerNotificationsAndResultCap(t *testing.T) {
	s := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) {
		return CallResult{Content: []Content{{Type: "text", Text: strings.Repeat("x", MaxServerResult+10)}}}, nil
	}}
	got := runServerMessages(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"+`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
	if strings.Contains(got, `"id":null`) {
		t.Fatalf("notification got a reply: %s", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("result was not capped: %s", got)
	}
}

func runServerMessages(t *testing.T, source ToolSource, input string) string {
	t.Helper()
	in, inputWriter := io.Pipe()
	r, w := io.Pipe()
	server := NewServer(source)
	conn := extproc.NewConnWith(in, w, server.Handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	defer conn.Close()
	go func() { _, _ = inputWriter.Write([]byte(input + "\n")) }()
	var output strings.Builder
	for i := 0; i < 1; i++ {
		line, err := readPipeLine(r)
		if err == nil {
			output.WriteString(line)
		}
	}
	return output.String()
}

func readPipeLine(r *io.PipeReader) (string, error) {
	b := make([]byte, 65536)
	n, err := r.Read(b)
	return string(b[:n]), err
}

// goldenExchange compares got with testdata/server/<name>.json
// (UPDATE_GOLDEN=1 writes it).
func goldenExchange(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "server", name+".json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("exchange mismatch\n got: %s\nwant: %s", got, want)
	}
}

// Spec (MS-6): the stateless revision needs no initialize; the revision
// and the client sit in the request's _meta.
func TestServerStatelessGolden(t *testing.T) {
	source := serverTestSource{
		list: []Tool{{Name: "read_file", Description: "Read a file", InputSchema: map[string]any{"type": "object"}}},
		call: func(ctx context.Context, name string, args map[string]any) (CallResult, error) {
			return CallResult{Content: []Content{{Type: "text", Text: "hi " + ClientName(ctx)}}}, nil
		},
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"codex","version":"9"}}`
	for name, input := range map[string]string{
		"stateless_tools_list":  `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`,
		"stateless_tools_call":  `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{},` + meta + `}}`,
		"server_discover":       `{"jsonrpc":"2.0","id":3,"method":"server/discover"}`,
		"stateless_bad_version": `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2031-01-01"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			goldenExchange(t, name, runServerMessages(t, source, input))
		})
	}
}
