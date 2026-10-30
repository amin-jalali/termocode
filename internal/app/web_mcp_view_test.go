package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webTestMCP(t *testing.T, frames []map[string]any) (webproto.MCPSnap, int) {
	t.Helper()
	var s webproto.MCPSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "mcp" {
			s = webproto.MCPSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEB-45, web design 2.9, 3.3): the mcp topic is the status
// screen's data: listening, the connected agents (name, since, calls,
// asked) and the last calls of outside agents as readable rows, never
// the raw audit text, the arguments or the socket path. An agent that
// connects or calls sends it again; nothing new sends nothing. There is
// no mcp op at all: the web cannot connect, disconnect or allow.
func TestMCPStatusTopic(t *testing.T) {
	m := mcpServeStatusModel(t)
	now := webTestClock(t)
	webTestHub(t, m)
	c := webTestSub(t, m, "mcp")
	s, n := webTestMCP(t, webTestNow(t, c))
	if n != 1 || !s.Listening || s.Mode != "auto" || len(s.Agents) != 1 {
		t.Fatalf("snap n=%d %+v", n, s)
	}
	if a := s.Agents[0]; a.Name != "claude-code" || a.Calls != 14 || a.Asked != 2 || a.Since == 0 {
		t.Errorf("agent %+v", a)
	}
	if len(s.Calls) != 2 || s.Calls[1] != "12:03 claude-code · tasks_run · asked, you allowed" {
		t.Errorf("calls %q", s.Calls)
	}
	raw, _ := webData(s)
	for _, bad := range []string{"gate=", "by=mcp:", "k.mcp.sock", "path=main.go", "git_status"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("%q in the snapshot: %s", bad, raw)
		}
	}
	for _, p := range webproto.Ops() {
		if strings.HasPrefix(p.Op, "mcp.") {
			t.Errorf("%s: the web cannot act on MCP", p.Op)
		}
	}

	m.webAfterUpdate(nil)
	if _, n := webTestMCP(t, webTestNow(t, c)); n != 0 {
		t.Fatal("sent with nothing new")
	}
	m.mcpServe.clients = append(m.mcpServe.clients, &mcpServeAgent{id: 2, since: time.Unix(1_700_000_000, 0)})
	m.webAfterUpdate(nil)
	if s, n = webTestMCP(t, webTestNow(t, c)); n != 1 || len(s.Agents) != 2 || s.Agents[1].Name != "" {
		t.Fatalf("after a connect n=%d %+v", n, s.Agents)
	}
	// The audit line of a call comes after the call: the next look sees it.
	m.output.Append(aiAuditChannel, "12:05:00  read_file  gate=auto  by=mcp:codex  → x")
	m.webAfterUpdate(nil)
	if _, n := webTestMCP(t, webTestNow(t, c)); n != 0 {
		t.Fatal("rebuilt on every Update")
	}
	*now = now.Add(3 * time.Second)
	m.webAfterUpdate(nil)
	if s, n = webTestMCP(t, webTestNow(t, c)); n != 1 || len(s.Calls) != 3 {
		t.Fatalf("after the audit line n=%d %q", n, s.Calls)
	}

	// Not listening: why, in one sentence.
	m.mcpServe = nil
	m.mcpServeStopped = true
	m.webAfterUpdate(nil)
	if s, n = webTestMCP(t, webTestNow(t, c)); n != 1 || s.Listening || s.Why != "You stopped it for this session." || len(s.Agents) != 0 {
		t.Fatalf("stopped n=%d %+v", n, s)
	}
}

// Spec (WEB-45, WEB-7): the mcp replay golden.
func TestMCPReplayGolden(t *testing.T) {
	m := mcpServeStatusModel(t)
	m.mcpServe.clients[0].since = time.Unix(1_700_000_000, 0) // not the local zone's date
	webTestClock(t)
	h := webTestHub(t, m)
	c := webReplayRecord(t, m, h, "mcp", func() {
		m.mcpServe.clients[0].calls++
		m.webAfterUpdate(nil)
		m.mcpServe.clients = nil
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["mcp"], c)
	webReplayGolden(t, webReplayPath("mcp", "agents"), c)
}
