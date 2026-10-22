package app

import (
	"errors"
	"os"
	"sync"

	"github.com/amin-jalali/termocode/internal/agents"
)

// ── The temp config file of an agent launch (M1 MS-4) ───────────────────
//
// Claude Code takes termocode's MCP server from a JSON file named on its
// command line (`--mcp-config`). The file lives in the private session
// dir (0700) next to the sockets, mode 0600, and holds only the path of
// this binary and of the workspace: no secret. It is written again at
// each launch, and removed when the agent's tab is gone and when
// termocode quits. The user's own config files are never touched.

// agentMCPLive are the temp files in existence, for StopMCPServer: the
// quit paths in cmd/termocode that do not pass through quitNow clean up
// too.
var (
	agentMCPLiveMu sync.Mutex
	agentMCPLive   = map[string]struct{}{}
)

func agentMCPTrack(path string, on bool) {
	agentMCPLiveMu.Lock()
	defer agentMCPLiveMu.Unlock()
	if on {
		agentMCPLive[path] = struct{}{}
	} else {
		delete(agentMCPLive, path)
	}
}

// removeAgentMCPLive removes every temp file written by this process.
func removeAgentMCPLive() {
	agentMCPLiveMu.Lock()
	defer agentMCPLiveMu.Unlock()
	for p := range agentMCPLive {
		_ = os.Remove(p)
		delete(agentMCPLive, p)
	}
}

// agentMCPFilePath is where the temp file of one recipe goes for root.
// Tests swap it. (agent_mcp_unix.go; other systems have no session dir.)
var agentMCPFilePath = agentMCPFilePathOS

// writeAgentMCPFile writes the temp config for agent a and remembers it
// for removal. The returned path goes on the agent's command line.
func (m *Model) writeAgentMCPFile(a agents.Agent, exe, root string) (string, error) {
	if a.MCP.TempFile == "" {
		return "", errors.New("recipe has no temp file")
	}
	path, err := agentMCPFilePath(root, a.MCP.TempFile)
	if err != nil {
		return "", err
	}
	// Remove first: an old file (or a link planted in its place) is
	// replaced, never written through.
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(mcpServeConfigJSON(exe, root))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return "", werr
	}
	if m.agentMCPFiles == nil {
		m.agentMCPFiles = map[string]string{}
	}
	m.agentMCPFiles[a.ID] = path
	agentMCPTrack(path, true)
	return path, nil
}

// pruneAgentMCPFiles removes the temp file of every agent that has no
// tab any more (shown or hidden).
func (m *Model) pruneAgentMCPFiles() {
	for id, path := range m.agentMCPFiles {
		if m.hasAgentTab(id) {
			continue
		}
		_ = os.Remove(path)
		agentMCPTrack(path, false)
		delete(m.agentMCPFiles, id)
	}
}

// removeAgentMCPFiles removes every temp file (quit).
func (m *Model) removeAgentMCPFiles() {
	for id, path := range m.agentMCPFiles {
		_ = os.Remove(path)
		agentMCPTrack(path, false)
		delete(m.agentMCPFiles, id)
	}
}

func (m *Model) hasAgentTab(id string) bool {
	for _, tabs := range [][]terminalTab{m.terminalTabs, m.hiddenTerminalTabs} {
		for _, t := range tabs {
			if t.Agent == id {
				return true
			}
		}
	}
	return false
}
