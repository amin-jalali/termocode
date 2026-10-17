package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// maxExtraAllocsMCP is what 30 MCP tools may add to an AI panel frame:
// the tools are built once per chat turn, never per frame, and the
// header chip is memoised, so the frame should cost the same.
const maxExtraAllocsMCP = 8

// perfMCPTools configures one MCP server with a cached list of 30 tools
// (nothing runs) and makes the Manager, as a chat turn would.
func perfMCPTools(t *testing.T, m *Model) int {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	user := mcpUserPath()
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte(`{"mcpServers":{"big":{"command":"termocode-perf-no-such-server"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	list, _ := mcp.Load(user, "")
	var tools []mcp.Tool
	for i := 0; i < 30; i++ {
		tools = append(tools, mcp.Tool{Name: fmt.Sprintf("tool_%02d", i), Description: strings.Repeat("Does a thing. ", 8),
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}})
	}
	b, _ := json.Marshal(map[string]any{"hash": list[0].Hash(), "server": map[string]string{"name": "big"}, "tools": tools})
	if err := os.MkdirAll(mcpCacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcpCacheDir(), "big.tools.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, st := range m.aiMCP().Tools(context.Background(), nil) {
		n += len(mcp.AITools(st.Server, st.Tools, nil, mcp.AdapterOptions{}))
	}
	return n
}

// Spec (MC-3 perf guard): an AI panel frame with 30 MCP tools in the
// list costs (about) the same allocations as one without MCP.
func TestPerfGuardAIPanelFrameMCPTools(t *testing.T) {
	perfColour(t)
	measure := func(withMCP bool) float64 {
		m := perfModel(t, true)
		if withMCP {
			if n := perfMCPTools(t, &m); n != 30 {
				t.Fatalf("%d MCP tools, want 30", n)
			}
			if !strings.Contains(m.View(), "0 MCP") {
				t.Fatal("MCP chip not shown")
			}
		}
		_ = m.View()
		i := 0
		return testing.AllocsPerRun(50, func() {
			i++
			m = cursorMove(m, i)
			_ = m.View()
		})
	}
	plain := measure(false)
	with := measure(true)
	if with > plain+maxExtraAllocsMCP || with > maxAllocsAIPanelFrame {
		t.Errorf("AI panel frame with 30 MCP tools: %.0f allocs, without: %.0f (extra limit %d, frame limit %d)",
			with, plain, maxExtraAllocsMCP, maxAllocsAIPanelFrame)
	} else {
		t.Logf("AI panel frame: %.0f allocs with 30 MCP tools, %.0f without", with, plain)
	}
}
