package mcp

import "encoding/json"

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
