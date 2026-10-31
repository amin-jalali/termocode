package mcp

import "encoding/json"

// Gateway (MCPX-36): "gateway": true on an entry of the user's own
// mcp.json lets `termocode mcp` re-serve the server's tools to outside
// agents (every call asks on a card, internal/app/mcp_serve_gateway.go).
// A project file can never opt in: a repository must not expose the
// reader's servers.
func (s Server) Gateway() bool {
	return s.Source == SourceUser && rawBool(s.raw, "gateway")
}

// rawBool reads a termocode boolean key of the entry ("" or not a bool:
// false). Keys read this way need no change to the entry struct.
func rawBool(raw json.RawMessage, key string) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	var b bool
	return json.Unmarshal(m[key], &b) == nil && b
}
