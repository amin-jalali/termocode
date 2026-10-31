package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// MCPX-10: the tokens a server's tool list costs each message is an
// estimate of its schemas; an off tool costs nothing.
func TestMCPToolTokens(t *testing.T) {
	srv := mcp.Server{Name: "s"}
	tools := []mcp.Tool{{Name: "a", Description: strings.Repeat("x", 400)}, {Name: "b", Description: "short"}}
	all := mcpToolTokens(srv, tools, mcp.Settings{})
	if all < 100 || all > 200 {
		t.Errorf("tokens = %d, want about 120", all)
	}
	off := mcpToolTokens(srv, tools, mcp.Settings{DisabledTools: map[string][]string{"s": {"a"}}})
	if off >= all/2 {
		t.Errorf("an off tool still counts: %d of %d", off, all)
	}
	for n, want := range map[int]string{0: "", 340: "~340 tokens", 1234: "~1.2k tokens", 23456: "~23k tokens"} {
		if got := mcpTokensText(n); got != want {
			t.Errorf("%d → %q, want %q", n, got, want)
		}
	}
}

// MCPX-10: the servers list and the server's screen show the cost; past
// the limit the list warns and says how to turn tools off.
func TestMCPTokenWarning(t *testing.T) {
	m := mcpFakeConnected(t)
	m.openMCPServers()
	box := ansi.Strip(m.picker.Box())
	if !strings.Contains(box, "tokens") || strings.Contains(box, "Turn off") {
		t.Fatalf("below the limit:\n%s", box)
	}
	if d := strings.Join(mcpDetailLines(m, "fake"), "\n"); !strings.Contains(d, "tokens") {
		t.Errorf("details lack the cost:\n%s", d)
	}
	old := mcpTokenWarn
	mcpTokenWarn = 10
	t.Cleanup(func() { mcpTokenWarn = old })
	m.openMCPServers()
	box = ansi.Strip(m.picker.Box())
	if !strings.Contains(box, "MCP tools use") || !strings.Contains(box, "Turn off") {
		t.Errorf("no warning:\n%s", box)
	}
}
