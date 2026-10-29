package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (SCN-13): Connect on an MCP server that can not be reached shows
// one toast whose title says "no connection" and whose line is the
// cause, so the short toast is not cut before the reason.
func TestMCPConnectUnreachableToast(t *testing.T) {
	m := &Model{w: 160, h: 40}
	err := fmt.Errorf("initialize: %w", &mcp.ConnError{Host: "127.0.0.1:9", Why: "it refused the connection. Is the server running?"})
	m.onMCPConnectDone(mcpConnectDoneMsg{name: "far", err: err})
	got := toastText(*m)
	if !strings.Contains(got, "MCP far: no connection") || !strings.Contains(got, "it refused the connection") || strings.Contains(got, "initialize:") {
		t.Errorf("toast:\n%s", got)
	}
}
