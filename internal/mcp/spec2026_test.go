package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (MCPX-20, 2026-07-28 routing headers): every HTTP request names
// its method in Mcp-Method; tools/call names the tool in Mcp-Name.
func TestHTTPRoutingHeaders(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	p := startHTTP(t, httpServer(f.MCPURL()), HTTPOptions{})
	if _, err := p.Client.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "hi"}); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.Requests() {
		if r.HTTPMethod != "POST" || strings.HasPrefix(r.RPC, "notifications/") {
			continue
		}
		if got := r.Header.Get("Mcp-Method"); got != r.RPC {
			t.Errorf("%s: Mcp-Method %q", r.RPC, got)
		}
		want := ""
		if r.RPC == "tools/call" {
			want = "echo"
		}
		if got := r.Header.Get("Mcp-Name"); got != want {
			t.Errorf("%s: Mcp-Name %q", r.RPC, got)
		}
	}
}

// Spec: a name that is not plain printable ASCII is left out of the
// header (never a broken request).
func TestRouteHeadersUnsafeName(t *testing.T) {
	h := routeHeaders("tools/call", map[string]any{"name": "a\r\nX-Evil: 1"})
	if h["Mcp-Method"] != "tools/call" || h["Mcp-Name"] != "" {
		t.Fatalf("%v", h)
	}
	h = routeHeaders("resources/read", map[string]any{"uri": "file:///a.txt"})
	if h["Mcp-Name"] != "file:///a.txt" {
		t.Fatalf("%v", h)
	}
}
