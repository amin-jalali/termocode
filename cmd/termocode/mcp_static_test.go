package main

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp"
)

type staticRig struct {
	inW  *io.PipeWriter
	out  *bufio.Reader
	errw *syncBuf
	done chan int
}

func newStaticRig(t *testing.T, tools []mcp.Tool, reason string) *staticRig {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	r := &staticRig{inW: inW, out: bufio.NewReader(outR), errw: &syncBuf{}, done: make(chan int, 1)}
	go func() {
		c := serveStaticMCP(inR, outW, r.errw, "/work/x", reason, tools)
		_ = outW.Close()
		r.done <- c
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return r
}

func (r *staticRig) ask(t *testing.T, line string) map[string]any {
	t.Helper()
	go io.WriteString(r.inW, line+"\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(readLine(t, r.out)), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var testTools = []mcp.Tool{{Name: "read_file", Description: "d"}, {Name: "diagnostics"}}

func TestStaticAnswersInitializeAndList(t *testing.T) {
	r := newStaticRig(t, testTools, "")
	m := r.ask(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"x"}}}`)
	res, _ := m["result"].(map[string]any)
	if res == nil || res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", m)
	}
	m = r.ask(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools, _ := m["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools/list: %v", m)
	}
	if !strings.Contains(r.errw.String(), "not open in /work/x") {
		t.Fatalf("stderr note missing: %q", r.errw.String())
	}
}

func TestStaticRefusesToolsCall(t *testing.T) {
	r := newStaticRig(t, testTools, "")
	m := r.ask(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{}}}`)
	e, _ := m["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32001) {
		t.Fatalf("got %v", m)
	}
	msg, _ := e["message"].(string)
	if !strings.Contains(msg, "termocode is not open in /work/x") || !strings.Contains(msg, "open termocode") {
		t.Fatalf("message %q", msg)
	}
}

func TestStaticReasonInMessage(t *testing.T) {
	r := newStaticRig(t, testTools, "permission denied")
	m := r.ask(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file"}}`)
	msg, _ := m["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "permission denied") {
		t.Fatalf("message %q", msg)
	}
}

func TestStaticExitsOnStdinEOF(t *testing.T) {
	r := newStaticRig(t, testTools, "")
	_ = r.inW.Close()
	select {
	case c := <-r.done:
		if c != 0 {
			t.Fatalf("exit %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not exit")
	}
}

// The real static list is the exported allow-list.
func TestStaticDefaultToolsNotEmpty(t *testing.T) {
	if len(staticMCPTools()) == 0 {
		t.Fatal("no static tools")
	}
}
