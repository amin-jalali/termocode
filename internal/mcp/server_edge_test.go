package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// errCode reads the JSON-RPC error code and message of a response line.
func errCode(t *testing.T, line string) (int, string) {
	t.Helper()
	var r struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil || r.Error == nil {
		t.Fatalf("not an error answer: %s", line)
	}
	return r.Error.Code, r.Error.Message
}

type unavailableErr struct{}

func (unavailableErr) Error() string     { return "closed" }
func (unavailableErr) Unavailable() bool { return true }

// Spec (MS-1): bad requests get "invalid params" with the reason; a
// closed workspace is "unavailable" for every kind of request.
func TestServerRequestErrors(t *testing.T) {
	ok := serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) { return CallResult{}, nil }}
	cases := []struct {
		name   string
		source ToolSource
		line   string
		code   int
		msg    string
	}{
		{"no params", ok, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`, -32602, "params are required"},
		{"no name", ok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"  "}}`, -32602, "name is required"},
		{"bad initialize", ok, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":null}`, -32602, "params are required"},
		{"no uri", resourceTestSource{}, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":""}}`, -32602, "uri is required"},
		{"call closed", nil, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`, -32001, "not open"},
		{"list resources closed", nil, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, -32001, "not open"},
		{"read closed", nil, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"x://y"}}`, -32001, "not open"},
		{"other version", ok, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1999-01-01"}}}`, -32602, "unsupported protocol version 1999-01-01"},
		{"unavailable error", serverTestSource{call: func(context.Context, string, map[string]any) (CallResult, error) {
			return CallResult{}, unavailableErr{}
		}},
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`, -32001, "not open"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runServerLines(t, c.source, 1, c.line)
			code, msg := errCode(t, got[0])
			if code != c.code || !strings.Contains(msg, c.msg) {
				t.Fatalf("error %d %q, want %d %q", code, msg, c.code, c.msg)
			}
		})
	}
}

// failingResources is a resource source whose reads fail.
type failingResources struct {
	serverTestSource
	err error
}

func (failingResources) Resources() []Resource { return []Resource{{URI: "x://a", Name: "a"}} }
func (f failingResources) ReadResource(context.Context, string) (string, error) {
	return "", f.err
}

// Spec (MS-1): a failed resources/read is "unavailable" when the
// workspace closed, else a tool failure with the reason.
func TestServerResourceReadErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
		msg  string
	}{
		{ErrToolSourceUnavailable, -32001, "not open"},
		{errors.New("disk read failed"), -32000, "disk read failed"},
	} {
		got := runServerLines(t, failingResources{err: c.err}, 1, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"x://a"}}`)
		code, msg := errCode(t, got[0])
		if code != c.code || !strings.Contains(msg, c.msg) {
			t.Fatalf("%v: error %d %q", c.err, code, msg)
		}
	}
}

// Spec (MS-1): notifications/cancelled stops the running call it names;
// unknown ids and broken params are ignored.
func TestServerCancelStopsCall(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	src := serverTestSource{call: func(ctx context.Context, _ string, _ map[string]any) (CallResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
			return CallResult{}, ctx.Err()
		case <-time.After(10 * time.Second):
			return CallResult{Content: []Content{{Type: "text", Text: "not cancelled"}}}, nil
		}
	}}
	in, inW := io.Pipe()
	r, w := io.Pipe()
	conn := extproc.NewConnWith(in, w, NewServer(src).Handle, extproc.ConnOptions{Framing: extproc.FramingNewline})
	defer conn.Close()
	send := func(l string) {
		if _, err := inW.Write([]byte(l + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"slow"}}`)
	<-started
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":"broken"}`)
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":99}}`)
	send(`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)
	send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not see the cancel")
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := errCode(t, line); code != -32000 {
		t.Fatalf("answer %s", line)
	}
}

// Spec (MS-1): the result cap keeps the first text up to the limit with
// a "truncated" mark, never splits a character, empties later text, and
// leaves other content alone.
func TestCapServerResult(t *testing.T) {
	big := strings.Repeat("é", MaxServerResult) // 2 bytes each
	r := capServerResult(CallResult{Content: []Content{
		{Type: "text", Text: "head"},
		{Type: "image", Data: "AAAA"},
		{Type: "text", Text: big},
		{Type: "text", Text: "tail"},
	}})
	if r.Content[0].Text != "head" || r.Content[1].Data != "AAAA" || r.Content[3].Text != "" {
		t.Fatalf("content %q %q %q", r.Content[0].Text, r.Content[1].Data, r.Content[3].Text)
	}
	cut := r.Content[2].Text
	if !strings.HasSuffix(cut, "… [truncated]") || !utf8.ValidString(cut) || len(r.Content[0].Text)+len(cut) > MaxServerResult {
		t.Fatalf("cut text: %d bytes, valid %v", len(cut), utf8.ValidString(cut))
	}

	full := strings.Repeat("x", MaxServerResult-3)
	r = capServerResult(CallResult{Content: []Content{{Type: "text", Text: full}, {Type: "text", Text: "more than three"}}})
	if r.Content[0].Text != full || r.Content[1].Text != "" {
		t.Fatalf("no room for the mark: %q", r.Content[1].Text)
	}
}
