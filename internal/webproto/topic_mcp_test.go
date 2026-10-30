package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-45): the mcp topic is typed for the page: termocode as an
// MCP server, its connected agents and their last calls, read only.
func TestMCPDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "mcp": { snap: MCPSnap; delta: MCPSnap };`,
		`  mode: "auto" | "on" | "off";`,
		`export interface MCPAgent {`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
