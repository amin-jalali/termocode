//go:build unix

package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-5 (4e): the MCP server of a container workspace listens on the
// socket keyed on the workspace ID (what `termocode mcp --workspace`
// finds), never on the local folder of the same path.
func TestMCPServeSocketOfAContainerWorkspaceIsKeyedOnItsID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	id := "container:default/api-1:/app"
	old := processWS.Load()
	SetWorkspace(workspace.Workspace{ID: id, Kind: workspace.KindContainer, Root: "/app"})
	t.Cleanup(func() { processWS.Store(old) })
	got, err := mcpServeSocketPath(aiWorkspaceRoot())
	if err != nil {
		t.Fatal(err)
	}
	want, _ := session.PathsForID(id)
	if got != want.MCPSocket {
		t.Fatalf("socket %q, want %q (keyed on the workspace ID)", got, want.MCPSocket)
	}
}
