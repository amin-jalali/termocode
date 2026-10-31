package mcp

import (
	"encoding/json"
	"os"
)

// Info is what the server said in initialize: its name and version, and
// the MCP version spoken (proto, "" when it does not run now: then the
// name and version come from the tool cache). ok=false: never connected.
// It never starts the server (the UI's server details).
func (m *Manager) Info(name string) (info ServerInfo, proto string, ok bool) {
	m.mu.Lock()
	st, found := m.servers[name]
	var p *Process
	var hash string
	if found {
		p, hash = st.proc, st.cfg.Hash()
	}
	m.mu.Unlock()
	if !found {
		return ServerInfo{}, "", false
	}
	if p != nil && p.Client != nil {
		return p.Client.ServerInfo(), p.Client.Version(), true
	}
	path := m.cachePath(name)
	if path == "" {
		return ServerInfo{}, "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ServerInfo{}, "", false
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Hash != hash || c.Server.Name == "" {
		return ServerInfo{}, "", false
	}
	return c.Server, "", true
}
