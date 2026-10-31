package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (MCPX-21): a server row says when the server runs in a sandbox,
// and a sandbox problem shows as the reason.
func TestMCPStatusHintSandbox(t *testing.T) {
	st := mcp.Status{Name: "fs", Type: mcp.TypeStdio, State: "stopped", Trusted: true,
		Sandbox: "sandbox: bwrap · network off · workspace read-only"}
	if got := mcpStatusHint(st); !strings.HasPrefix(got, "local · sandbox: bwrap · network off · workspace read-only · stopped") {
		t.Fatalf("hint %q", got)
	}
}
