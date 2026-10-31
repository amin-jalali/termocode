package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (AI-4, AI-6): chat turns keep big MCP results in a session file
// (in the temp folder), warn in the MCP log, and offer the reader tool
// only when there are MCP tools.
func TestMCPTurnSpillsBigResults(t *testing.T) {
	m := covMCPModel(t)
	m.output = newOutputStore()
	o := m.mcpAdapterOptions(mcp.Settings{MaxResultKB: 4})
	if o.Spill == nil || !strings.HasPrefix(o.Spill.Dir, os.TempDir()) || o.MaxResult != 4<<10 || o.Warn == nil {
		t.Fatalf("opts %+v", o)
	}
	o.Warn("srv", "big", 2<<20)
	if !strings.Contains(strings.Join(m.output.Lines(mcpLogChannel), "\n"), "srv: big sent 2048 KB") {
		t.Errorf("log %q", strings.Join(m.output.Lines(mcpLogChannel), "\n"))
	}
	o.Log("srv", "t: the result does not match its output schema")
	if !strings.Contains(strings.Join(m.output.Lines(mcpLogChannel), "\n"), "srv: t: the result does not match") {
		t.Errorf("log note missing")
	}
	if got := withMCPReader(nil, o.Spill); len(got) != 0 {
		t.Errorf("reader without MCP tools: %v", got)
	}
	got := withMCPReader([]ai.Tool{{Name: "mcp__a__b"}}, o.Spill)
	if len(got) != 2 || got[1].Name != mcp.ResultReadTool {
		t.Errorf("tools %v", got)
	}
}
