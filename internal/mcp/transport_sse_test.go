package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (CF-11): a server of the old HTTP+SSE transport answers the
// Streamable HTTP POST with a 4xx; termocode then opens its event
// stream, connects and marks it "old transport".
func TestLegacySSEFallback(t *testing.T) {
	f := mcptest.NewLegacySSEServer(t)
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"old": map[string]any{"type": "http", "url": f.SSEURL()}})
	var lines logLines
	m := e.httpManager(t, nil, nil, 0)
	m.opts.Log = func(_, l string) { lines.add(l) }
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d %+v", n, status(m, "old"))
	}
	st := status(m, "old")
	if st.State != StateRunning || !st.OldTransport {
		t.Errorf("status %+v", st)
	}
	r, err := m.Call(context.Background(), nil, "old", "echo", map[string]any{"text": "legacy"})
	if err != nil || r.Text() != "echo: legacy" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	if !lines.has("old transport") {
		t.Errorf("log %v", lines.lines)
	}
	m.Stop("old")
	if st := status(m, "old"); st.State != StateStopped {
		t.Errorf("after stop %+v", st)
	}
}

// Spec (CF-11): the stream's endpoint must be on the server's own
// origin (a stream can not send our token elsewhere).
func TestLegacyEndpointSameOrigin(t *testing.T) {
	for in, ok := range map[string]bool{
		"/messages?s=1":                 true,
		"messages":                      true,
		"http://127.0.0.1:9/messages":   true,
		"https://evil.example/messages": false,
		"//evil.example/messages":       false,
		"http://127.0.0.1:10/messages":  false,
	} {
		got, err := legacyEndpoint("http://127.0.0.1:9/sse", in)
		if (err == nil) != ok {
			t.Errorf("%q: %q %v", in, got, err)
		}
		if ok && !strings.HasPrefix(got, "http://127.0.0.1:9/") {
			t.Errorf("%q → %q", in, got)
		}
	}
}
