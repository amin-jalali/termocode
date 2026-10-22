package app

import (
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Starting and stopping the MCP server's listener (MS-1). It runs in the
// UI process only while asked for; stopping closes the socket, every
// client connection, and says no to every waiting card.

// mcpServeTarget is the running listener, for StopMCPServer.
var mcpServeTarget atomic.Pointer[mcp.SocketServer]

// mcpServeTargetHub is its hub: StopMCPServer stops it first, so a
// connection that tells the UI it is gone does not wait for a UI that quit.
var mcpServeTargetHub atomic.Pointer[mcpServeHub]

// StopMCPServer closes the MCP server's socket and its connections.
// cmd/termocode calls it when Bubble Tea returns, next to StopMCP.
func StopMCPServer() {
	removeAgentMCPLive() // the agents' temp config files (agent_mcp.go)
	if h := mcpServeTargetHub.Swap(nil); h != nil {
		h.stop()
	}
	if s := mcpServeTarget.Swap(nil); s != nil {
		_ = s.Close()
	}
}

// startMCPServe starts the listener for the workspace (nothing when it
// runs already). An error is one toast and errors.log, never a crash.
func (m *Model) startMCPServe() tea.Cmd {
	if m.mcpServe != nil {
		return nil
	}
	root := aiWorkspaceRoot()
	path, err := mcpServeSocketPath(root)
	if err == nil {
		var srv *mcp.SocketServer
		if ln, lerr := mcpServeListen(path); lerr != nil {
			err = lerr
		} else {
			hub := newMCPServeHub()
			src := newMCPServeSource(hub, root)
			srv = mcp.Serve(ln, src, mcpServePeer, func(err error) {
				recordError("[mcp-serve] " + err.Error())
			})
			m.mcpServe = &mcpServeState{hub: hub, srv: srv, src: src, path: path}
			m.mcpServeErr = ""
			mcpServeTarget.Store(srv)
			mcpServeTargetHub.Store(hub)
			return waitMCPServe(hub)
		}
	}
	recordError("[mcp-serve] not started: " + err.Error())
	m.mcpServeErr = ai.Redact(err.Error())
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, "MCP server not started", err.Error())
	return c
}

// stopMCPServe stops the listener (nothing when it does not run).
func (m *Model) stopMCPServe() {
	st := m.mcpServe
	if st == nil {
		return
	}
	m.mcpServe = nil
	if m.confirmOpen && m.confirmKind == confirmKindMCPServe {
		m.confirmOpen = false
	}
	st.declineAll()
	st.hub.stop()
	if st.srv != nil {
		mcpServeTarget.CompareAndSwap(st.srv, nil)
		mcpServeTargetHub.CompareAndSwap(st.hub, nil)
		_ = st.srv.Close()
	}
}

// mcpServeMode is the mcp_server setting: "auto" (the default: serve
// while an agent tab asks for it), "on" (serve from start) or "off"
// (never). Anything else is auto.
func mcpServeMode() string {
	switch v := strings.ToLower(strings.TrimSpace(loadSettings().MCPServer)); v {
	case "on", "off":
		return v
	}
	return "auto"
}

// syncMCPServe starts or stops the listener to match mcp_server.
func (m *Model) syncMCPServe() tea.Cmd {
	want := false
	switch mcpServeMode() {
	case "on":
		want = true
	case "auto":
		want = m.mcpServeForAgent
	}
	if !want {
		m.stopMCPServe()
		return nil
	}
	return m.startMCPServe()
}

// ensureMCPServeForAgent starts the listener for an agent tab unless
// mcp_server is off (MS-4 calls it before it launches the agent).
func (m *Model) ensureMCPServeForAgent() tea.Cmd {
	if mcpServeMode() == "off" {
		return nil
	}
	m.mcpServeForAgent = true
	return m.startMCPServe()
}

// onMCPServeConfigSaved applies mcp_server when config.json is saved
// (it runs for every saved file: the tool list follows connection files).
func (m *Model) onMCPServeConfigSaved(path string) tea.Cmd {
	m.mcpServeToolsCheck() // a connection file may have been saved
	want, err := settingsConfigPath()
	if err != nil || filepath.Clean(path) != filepath.Clean(want) {
		return nil
	}
	m.mcpServeAllowSync() // Always allow edited by hand
	return m.syncMCPServe()
}
