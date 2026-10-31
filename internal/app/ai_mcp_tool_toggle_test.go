package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// MCPX-3: a tool turned off in Show tools is never sent to the model;
// the other tools of the server still are.
func TestMCPTurnToolsSkipsDisabled(t *testing.T) {
	srv := mcp.Server{Name: "fake", Type: mcp.TypeStdio, Source: mcp.SourceUser}
	yes := true
	ro := &mcp.ToolAnnotations{ReadOnlyHint: &yes}
	list := []mcp.ServerTools{{Server: srv, Tools: []mcp.Tool{{Name: "echo", Annotations: ro}, {Name: "write_note"}}}}
	set := mcp.Settings{TrustAnnotations: true, DisabledTools: map[string][]string{"fake": {"write_note"}}}
	var names []string
	for _, tl := range mcpTurnTools(list, set, nil, mcp.AdapterOptions{}) {
		names = append(names, tl.Display)
	}
	if strings.Join(names, ",") != "mcp.fake.echo" {
		t.Errorf("tools sent = %v", names)
	}
	set.DisabledTools = nil
	if got := len(mcpTurnTools(list, set, nil, mcp.AdapterOptions{})); got != 2 {
		t.Errorf("with nothing off: %d tools", got)
	}
	var _ []ai.Tool = mcpTurnTools(nil, set, nil, mcp.AdapterOptions{})
}
