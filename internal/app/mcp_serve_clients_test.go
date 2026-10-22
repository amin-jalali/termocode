//go:build unix

package app

import (
	"bufio"
	"context"
	"net"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

func mcpServeCtxID(client string, id uint64) context.Context {
	return mcp.WithConnID(mcp.WithClientName(context.Background(), client), id)
}

// Spec (MS-3): the listener tracks each connected agent (name from
// initialize, calls) and forgets it when it goes; the chip count follows
// only these events.
func TestMCPServeClientsTracked(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	m.startMCPServe()
	defer m.stopMCPServe()
	if n := m.mcpServeAgentCount(); n != 0 {
		t.Fatalf("agents before any connect = %d", n)
	}
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"fake client"}}}`)
	mcpServePumpUntil(t, m, m.mcpServe.hub, "client named", func() bool {
		return len(m.mcpServe.clients) == 1 && m.mcpServe.clients[0].name == "fake-client"
	})
	if m.mcpServeAgentCount() != 1 || m.mcpServe.clients[0].since.IsZero() {
		t.Fatalf("clients = %+v", m.mcpServe.clients)
	}
	mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"notify","arguments":{"title":"x"}}}`)
	mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"notify","arguments":{"title":"y"}}}`)
	mcpServePumpUntil(t, m, m.mcpServe.hub, "two calls counted", func() bool { return m.mcpServe.clients[0].calls == 2 })
	if got := m.mcpServe.clients[0]; got.asked != 0 {
		t.Errorf("asked = %d for calls with no card", got.asked)
	}
	_ = c.Close()
	mcpServePumpUntil(t, m, m.mcpServe.hub, "client gone", func() bool { return len(m.mcpServe.clients) == 0 })
	if m.mcpServeAgentCount() != 0 {
		t.Error("a closed client still counts")
	}
}

// Spec (MS-3): a call that showed a card counts as asked.
func TestMCPServeClientAskedCounted(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	m.mcpServe.clients = []*mcpServeAgent{{id: 7, name: "claude-code"}}
	ctx := mcpServeCtxID("claude-code", 7)
	ch := make(chan mcpServeTestCall, 1)
	go func() {
		r, err := src.Call(ctx, "debug_breakpoint", map[string]any{"op": "clear"})
		ch <- mcpServeTestCall{r, err}
	}()
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, ch)
	mcpServePumpUntil(t, m, hub, "counted", func() bool { return m.mcpServe.clients[0].calls == 1 })
	if got := m.mcpServe.clients[0]; got.asked != 1 {
		t.Errorf("client = %+v, want 1 call, 1 asked", got)
	}
}
