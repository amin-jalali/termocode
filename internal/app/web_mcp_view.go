package app

import (
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The mcp topic (WEB-45, web design 2.9): the laptop's "MCP: Server
// Status" screen for the page, read only: does termocode listen for
// outside agents, who is connected, their last calls. It has no op: the
// web cannot start or stop the server, disconnect an agent or allow a
// tool (web design 3.3). The snapshot is rebuilt when the server or its
// agents change (a cheap key on Update) and at most every webMCPEvery
// otherwise, for the call rows that the audit log gets after a call; it
// is sent only when it changed. Never a web timer.

const webMCPEvery = 2 * time.Second

func init() {
	webTopics["mcp"] = webTopicDef{snapshot: webMCPSnapshot, apply: webApplyReplace[webproto.MCPSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishMCP)
}

// webMCPDelta is an mcp delta: the whole snapshot.
type webMCPDelta webproto.MCPSnap

func (webMCPDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webMCPDelta)
	return n, ok
}

// webMCPPub is what the mcp topic sent last (Update only).
type webMCPPub struct {
	last  *webproto.MCPSnap
	key   webMCPKey
	built time.Time
}

// webMCPKey changes when the server or an agent changes.
type webMCPKey struct {
	st              *mcpServeState
	stopped         bool
	err             string
	agents, calls   int
	asked, namedLen int
}

func (m *Model) webMCPKey() webMCPKey {
	k := webMCPKey{st: m.mcpServe, stopped: m.mcpServeStopped, err: m.mcpServeErr}
	if st := m.mcpServe; st != nil {
		k.agents = len(st.clients)
		for _, a := range st.clients {
			k.calls += a.calls
			k.asked += a.asked
			k.namedLen += len(a.name)
		}
	}
	return k
}

func webMCPSnapshot(m *Model, _ string) (any, error) {
	snap := m.webMCPBuild()
	if m.web != nil {
		p := &m.web.more.mcp
		p.last, p.key, p.built = &snap, m.webMCPKey(), webNow()
	}
	return snap, nil
}

// webPublishMCP sends the snapshot when it changed.
func (m *Model) webPublishMCP(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.more.mcp
	if !h.webListens("mcp") {
		p.last = nil
		return nil
	}
	if p.last == nil {
		return nil
	}
	now, key := webNow(), m.webMCPKey()
	if key == p.key && now.Sub(p.built) < webMCPEvery {
		return nil
	}
	p.key, p.built = key, now
	cur := m.webMCPBuild()
	if reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("mcp", webMCPDelta(cur))
	return nil
}

// webMCPBuild is the status screen's data.
func (m *Model) webMCPBuild() webproto.MCPSnap {
	s := webproto.MCPSnap{Mode: mcpServeMode(), Agents: []webproto.MCPAgent{}, Calls: []string{}}
	if st := m.mcpServe; st != nil {
		s.Listening = true
		for i, a := range st.clients {
			if i == webproto.MaxMCPAgents {
				break
			}
			s.Agents = append(s.Agents, webproto.MCPAgent{Name: webName(a.name), Since: a.since.UnixMilli(), Calls: a.calls, Asked: a.asked})
		}
	} else {
		_, s.Why = m.mcpServeNotListeningWhy()
	}
	for _, l := range m.mcpServeAuditLines(webproto.MaxMCPCalls) {
		if row, ok := mcpServeCallRow(l); ok {
			s.Calls = append(s.Calls, webName(row))
		}
	}
	return s
}
