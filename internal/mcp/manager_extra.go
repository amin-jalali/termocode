package mcp

import (
	"encoding/json"
	"os"
)

// AutostartOff: "autostart": false — the server never starts just to list
// its tools (RT-2). Its tools are offered once the user connects it
// (Connect in the servers list) or a list is saved from an earlier run.
func (s Server) AutostartOff() bool {
	if len(s.raw) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(s.raw, &m) != nil {
		return false
	}
	var b bool
	return json.Unmarshal(m["autostart"], &b) == nil && !b
}

// ResetToolCache forgets the saved tool lists (RT-14): one server's, or
// every server's when name is "". A stopped server lists its tools again
// on next use; a running one keeps its live list. It returns how many
// saved lists were removed.
func (m *Manager) ResetToolCache(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for sname, st := range m.servers {
		if name != "" && sname != name {
			continue
		}
		if p := m.cachePath(sname); p != "" && os.Remove(p) == nil {
			n++
		}
		if st.proc == nil {
			st.tools, st.haveTools, st.cached, st.toolsHash = nil, false, false, ""
		}
	}
	return n
}
