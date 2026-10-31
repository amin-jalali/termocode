package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Big MCP results (AI-4, AI-6): kept whole in this session's temp
// folder; the model reads them on with mcp_read_result.

var (
	mcpSpillOnce sync.Once
	mcpSpillDir  *mcp.Spill
)

func mcpSpill() *mcp.Spill {
	mcpSpillOnce.Do(func() {
		mcpSpillDir = mcp.NewSpill(filepath.Join(os.TempDir(), fmt.Sprintf("termocode-mcp-results-%d", os.Getpid())))
	})
	return mcpSpillDir
}

// mcpAdapterOptions are the adapter options of a chat turn.
func (m *Model) mcpAdapterOptions(set mcp.Settings) mcp.AdapterOptions {
	out := m.output
	return mcp.AdapterOptions{TrustAnnotations: set.TrustAnnotations, MaxResult: set.MaxResultKB << 10, Clean: aiCleanText,
		Spill: mcpSpill(),
		Warn: func(server, tool string, size int) {
			out.Append(mcpLogChannel, clock.Now().Format("15:04:05")+"  "+server+": "+
				fmt.Sprintf("%s sent %d KB: the AI got the first part and can read the rest", tool, size>>10))
		},
		Log: func(server, line string) {
			out.Append(mcpLogChannel, clock.Now().Format("15:04:05")+"  "+server+": "+ai.Redact(aiCleanText(line)))
		}}
}

// withMCPReader adds the saved-result reader when there are MCP tools.
func withMCPReader(tools []ai.Tool, sp *mcp.Spill) []ai.Tool {
	if len(tools) == 0 || sp == nil {
		return tools
	}
	return append(tools, sp.Tool())
}
