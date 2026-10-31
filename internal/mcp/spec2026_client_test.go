package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (MCPX-20): a server that answers server/discover with the
// stateless revision is used statelessly: no initialize, every request
// carries the revision and termocode's name in _meta.
func TestModernStatelessHandshake(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	p := startFake(t, map[string]string{"FAKE_MODERN": "only", "FAKE_LOG": log}, Options{})
	if v := p.Client.Version(); v != StatelessVersion {
		t.Fatalf("version %q, want %s", v, StatelessVersion)
	}
	if got := p.Client.ServerInfo(); got.Name != "fake-mcp-modern" || got.Version != "2.0.0" {
		t.Fatalf("server info %+v", got)
	}
	if !p.Client.Capabilities().Prompts {
		t.Fatal("capabilities from server/discover were not read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.Client.ListTools(ctx); err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	res, err := p.Client.CallTool(ctx, "echo", map[string]any{"text": "hi"})
	if err != nil || res.Text() != "echo: hi" {
		t.Fatalf("call: %q %v", res.Text(), err)
	}
	if err := p.Client.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	got := fakeLog(t, log)
	if strings.Contains(got, "initialize") {
		t.Fatalf("a stateless server got initialize:\n%s", got)
	}
	for _, want := range []string{"meta tools/list 2026-07-28 termocode", "meta tools/call 2026-07-28 termocode"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// Spec (MCPX-20): an older server answers server/discover with an
// error; termocode falls back to the initialize handshake.
func TestModernProbeFallsBackToInitialize(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	p := startFake(t, map[string]string{"FAKE_LOG": log}, Options{})
	if v := p.Client.Version(); v != ProtocolVersion {
		t.Fatalf("version %q, want %s", v, ProtocolVersion)
	}
	got := fakeLog(t, log)
	if !strings.Contains(got, "server/discover\ninitialize\n") {
		t.Fatalf("want the probe, then the handshake:\n%s", got)
	}
}

// Spec (MCPX-20): over HTTP the stateless revision goes in the
// MCP-Protocol-Version header and in _meta; there is no session.
func TestModernHTTP(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Modern: true, Session: false})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	if !p.Client.Stateless() || p.Client.ServerInfo().Name != "fake-mcp-modern" {
		t.Fatalf("stateless %v info %+v", p.Client.Stateless(), p.Client.ServerInfo())
	}
	r, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil || r.Text() != "echo: hi" {
		t.Fatalf("call %q %v", r.Text(), err)
	}
	if got := strings.Join(rpcs(f), ","); !strings.HasPrefix(got, "server/discover,") || !strings.Contains(got, "tools/call") {
		t.Fatalf("rpcs %s", got)
	}
	for _, q := range f.Requests() {
		if q.Header.Get("MCP-Protocol-Version") != StatelessVersion || !strings.Contains(q.Body, `"io.modelcontextprotocol/protocolVersion":"2026-07-28"`) {
			t.Errorf("%s: header %q body %s", q.RPC, q.Header.Get("MCP-Protocol-Version"), q.Body)
		}
	}
}

// Spec (MCPX-20, MRTR): a stateless server asks for input with
// resultType "input_required"; termocode asks the user through the same
// elicitation card, then sends the call again with the inputResponses
// and the server's requestState, and gets the final result.
func TestModernMRTRElicit(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	p := startFake(t, map[string]string{"FAKE_MODERN": "only", "FAKE_LOG": log}, Options{})
	var asked []ElicitRequest
	ctx := WithElicitor(context.Background(), func(_ context.Context, req ElicitRequest) ElicitResult {
		asked = append(asked, req)
		return ElicitResult{Action: ElicitAccept, Content: map[string]any{"name": "Ann"}}
	})
	res, err := p.Client.CallToolTimeout(ctx, "ask", nil, 5*time.Second)
	if err != nil || res.Text() != `accept {"name":"Ann"}` {
		t.Fatalf("call: %q %v", res.Text(), err)
	}
	if len(asked) != 1 || asked[0].Mode != ElicitForm || len(asked[0].Fields) != 1 || asked[0].Fields[0].Label() != "Your name" {
		t.Fatalf("asked %+v", asked)
	}
	if n := strings.Count(fakeLog(t, log), "meta tools/call"); n != 2 {
		t.Fatalf("%d tools/call, want the first and the retry", n)
	}
	// Two rounds: each retry carries only the newest answers.
	res, err = p.Client.CallTool(ctx, "ask_twice", nil)
	if err != nil || res.Text() != `two rounds: accept {"name":"Ann"}` {
		t.Fatalf("two rounds: %q %v", res.Text(), err)
	}
	// URL mode carries no content.
	asked = nil
	if res, err = p.Client.CallTool(ctx, "ask_url", nil); err != nil || res.Text() != "accept" || len(asked) != 1 || asked[0].URL == "" {
		t.Fatalf("url mode: %q %v %+v", res.Text(), err, asked)
	}
}

// Spec (MCPX-20, MRTR): without a UI the answer is decline; roots/list
// is answered with the workspace roots; sampling is refused on purpose.
func TestModernMRTRRootsDeclineSampling(t *testing.T) {
	root := t.TempDir()
	p := startFake(t, map[string]string{"FAKE_MODERN": "only"}, Options{Roots: func() []string { return []string{root} }})
	ctx := context.Background()
	if res, err := p.Client.CallTool(ctx, "ask", nil); err != nil || res.Text() != "decline" {
		t.Fatalf("no UI: %q %v", res.Text(), err)
	}
	if res, err := p.Client.CallTool(ctx, "roots", nil); err != nil || !strings.HasPrefix(res.Text(), "roots: file://") {
		t.Fatalf("roots: %q %v", res.Text(), err)
	}
	if _, err := p.Client.CallTool(ctx, "sample", nil); err == nil || !strings.Contains(err.Error(), "sampling") {
		t.Fatalf("sampling: %v", err)
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Spec (MCPX-20): a stateless connection opens subscriptions/listen for
// list changes; a change on that stream reaches ToolsChanged /
// ResourcesChanged. When the server ends the stream, termocode listens
// again.
func TestModernSubscriptionsListen(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	var tools, resources atomic.Int64
	p := startFake(t, map[string]string{"FAKE_MODERN": "only", "FAKE_LOG": log}, Options{
		ToolsChanged:     func() { tools.Add(1) },
		ResourcesChanged: func(string) { resources.Add(1) },
	})
	listens := func() int { return strings.Count(fakeLog(t, log), "\nsubscriptions/listen\n") }
	waitUntil(t, "the listen request", func() bool { return listens() == 1 })
	ctx := context.Background()
	if res, err := p.Client.CallTool(ctx, "notify_changed", nil); err != nil || res.Text() != "notified" {
		t.Fatalf("notify_changed: %q %v", res.Text(), err)
	}
	waitUntil(t, "tools changed", func() bool { return tools.Load() == 1 })
	if res, err := p.Client.CallTool(ctx, "notify_resources", nil); err != nil || res.Text() != "notified" {
		t.Fatalf("notify_resources: %q %v", res.Text(), err)
	}
	waitUntil(t, "resources changed", func() bool { return resources.Load() == 1 })
	if _, err := p.Client.CallTool(ctx, "end_listen", nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "a new listen request", func() bool { return listens() == 2 })
}

// Spec (MCPX-20): over HTTP the change stream is the long answer of the
// subscriptions/listen POST.
func TestModernHTTPListen(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Modern: true})
	var tools atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := StartHTTP(ctx, httpServer(f.MCPURL()), HTTPOptions{}, Options{ToolsChanged: func() { tools.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	waitUntil(t, "the listen request", func() bool { return strings.Contains(strings.Join(rpcs(f), ","), "subscriptions/listen") })
	// The stream may still be opening: notify until a change arrives.
	waitUntil(t, "tools changed", func() bool {
		if tools.Load() > 0 {
			return true
		}
		_, _ = p.Client.CallTool(ctx, "notify_changed", nil)
		return tools.Load() > 0
	})
}
