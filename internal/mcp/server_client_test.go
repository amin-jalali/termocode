package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// runServerLines sends every line and returns n response lines.
func runServerLines(t *testing.T, source ToolSource, n int, lines ...string) []string {
	t.Helper()
	in, inputWriter := io.Pipe()
	r, w := io.Pipe()
	server := NewServer(source)
	conn := extproc.NewConnWith(in, w, server.Handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	defer conn.Close()
	go func() {
		for _, l := range lines {
			_, _ = inputWriter.Write([]byte(l + "\n"))
		}
	}()
	var out []string
	buf := make([]byte, 65536)
	var pending string
	for len(out) < n {
		k, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		pending += string(buf[:k])
		for {
			i := strings.IndexByte(pending, '\n')
			if i < 0 {
				break
			}
			out = append(out, pending[:i])
			pending = pending[i+1:]
		}
	}
	return out
}

// Spec (MS-1): the client name from initialize reaches tools/call, so the
// UI can name the client in its audit line and confirm card.
func TestServerClientNameReachesCall(t *testing.T) {
	got := make(chan string, 1)
	source := serverTestSource{call: func(ctx context.Context, _ string, _ map[string]any) (CallResult, error) {
		got <- ClientName(ctx)
		return CallResult{Content: []Content{{Type: "text", Text: "ok"}}}, nil
	}}
	runServerLines(t, source, 2,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","version":"2"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
	if name := <-got; name != "claude-code" {
		t.Fatalf("client name = %q, want claude-code", name)
	}
	if ClientName(context.Background()) != "" {
		t.Fatal("a bare context has a client name")
	}
}

// Spec (MS-1): a ToolSource error with a code keeps that code (declined
// -32000, timeout -32002, invalid arguments -32602).
func TestServerCallErrorCode(t *testing.T) {
	for _, code := range []int{CodeDeclined, CodeTimeout, CodeInvalidParams} {
		source := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) {
			return CallResult{}, &CallError{Code: code, Message: "nope"}
		}}
		lines := runServerLines(t, source, 1, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
		var resp struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    struct {
					Reason string `json:"reason"`
				} `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error.Code != code || resp.Error.Message != "nope" {
			t.Errorf("code %d: got %s", code, lines[0])
		}
		// Declined says why in data (fix 3); the other codes need no
		// reason (-32002 is also "resource not found").
		want := map[int]string{CodeDeclined: ReasonDeclined}[code]
		if resp.Error.Data.Reason != want {
			t.Errorf("code %d: data.reason = %q, want %q (%s)", code, resp.Error.Data.Reason, want, lines[0])
		}
	}
}

// Spec (fix 3): declined and "the tool failed" both use -32000 (the MCP spec
// reserves no code for either), told apart by data.reason.
func TestServerToolFailedHasReason(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		reason string
	}{
		"declined":    {&CallError{Code: CodeDeclined, Message: "declined by the user"}, "declined"},
		"tool failed": {errors.New("boom"), "tool_error"},
	} {
		source := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) { return CallResult{}, tc.err }}
		lines := runServerLines(t, source, 1, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
		var resp struct {
			Error struct {
				Code int             `json:"code"`
				Data json.RawMessage `json:"data"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error.Code != -32000 || string(resp.Error.Data) != `{"reason":"`+tc.reason+`"}` {
			t.Errorf("%s: got %s", name, lines[0])
		}
	}
}
