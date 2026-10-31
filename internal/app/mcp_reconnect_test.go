package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (RT-6): a remote server that waits for a reconnect shows
// "reconnecting in Ns" and why, in the plain and the service hint.
func TestMCPReconnectingHint(t *testing.T) {
	covMCPModel(t)
	st := mcp.Status{Name: "x", Type: mcp.TypeHTTP, State: mcp.StateReconnecting, RetryIn: 3500 * time.Millisecond,
		Problem: "no connection to h: refused", Trusted: true}
	if got := mcpStatusHint(st); !strings.HasPrefix(got, "remote · reconnecting in 4s (no connection to h: refused)") {
		t.Errorf("hint %q", got)
	}
	writeUserMCP(t, `{"mcpServers":{"linear":{"type":"http","url":"https://mcp.linear.app/mcp"}}}`)
	st.Name = "linear"
	if got := mcpStatusHint(st); !strings.Contains(got, "reconnecting in 4s") {
		t.Errorf("service hint %q", got)
	}
}

// Spec (RT-5): "AI: MCP: Reconnect All Servers" is in the palette, works
// before a provider is set, and says what it did.
func TestMCPReconnectAllPalette(t *testing.T) {
	m := covMCPModel(t)
	found := false
	for _, e := range aiPaletteItems() {
		found = found || (e.id == "ai-mcp-reconnect-all" && e.title == "AI: MCP: Reconnect All Servers")
	}
	if !found {
		t.Fatal("no palette entry")
	}
	cmd, ok := m.dispatchAIPalette("ai-mcp-reconnect-all")
	if !ok || cmd == nil {
		t.Fatal("not dispatched")
	}
	if c := m.onMCPReconnectAllDone(mcpReconnectAllDoneMsg{}); c == nil {
		t.Error("no toast for nothing to do")
	}
	if got := mcpReconnectAllText(mcp.ReconnectResult{}); got != "MCP: every server is fine" {
		t.Errorf("text %q", got)
	}
	if got := mcpReconnectAllText(mcp.ReconnectResult{Tried: 3, OK: 2, Failed: []string{"lin"}, Checked: 1}); got != "MCP: 2 of 3 servers back · still down: lin" {
		t.Errorf("text %q", got)
	}
}
