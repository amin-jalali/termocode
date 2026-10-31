package mcp

import (
	"context"
	"path/filepath"
	"strings"
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
	if got := strings.Join(rpcs(f), ","); got != "server/discover,tools/call" {
		t.Fatalf("rpcs %s", got)
	}
	for _, q := range f.Requests() {
		if q.Header.Get("MCP-Protocol-Version") != StatelessVersion || !strings.Contains(q.Body, `"io.modelcontextprotocol/protocolVersion":"2026-07-28"`) {
			t.Errorf("%s: header %q body %s", q.RPC, q.Header.Get("MCP-Protocol-Version"), q.Body)
		}
	}
}
