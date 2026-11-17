//go:build unix

package app

import (
	"strings"
	"testing"
)

// The GIFs' dev action afteradddemo:mcp opens the real after-add card of
// a made-up MCP server over a fake probe: the findings, Review tools as
// the next step, Not now first.
func TestAfterAddDemoCard(t *testing.T) {
	m := agentMCPTestModel(t)
	cmd := m.runDevAction("afteradddemo:mcp")
	if cmd == nil {
		t.Fatal("afteradddemo:mcp returned no command")
	}
	msg, ok := cmd().(afterAddMsg)
	if !ok {
		t.Fatal("not an afterAddMsg")
	}
	m.onAfterAdd(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd {
		t.Fatal("no after-add card")
	}
	view := strings.Join(strings.Fields(m.confirm.View()), " ")
	for _, want := range []string{"Connected postgres: review its tools?", "connected, answered in 84 ms",
		"5 tools: 3 read-only, 1 write, 1 unknown", "same names as termocode's own tools: read_file",
		"the PostgreSQL connection shop", "Not now", "Review tools"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}
}
