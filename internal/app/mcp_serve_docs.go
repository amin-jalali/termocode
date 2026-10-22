package app

import (
	"sort"

	"github.com/amin-jalali/termocode/internal/ai"
)

// MCP server gate words for the generated docs (cmd/docgen).
const (
	MCPServeNo   = "no"       // not offered to outside agents
	MCPServeYes  = "yes"      // offered, runs at once
	MCPServeAsks = "asks"     // offered, every call asks on a card
	MCPServeSome = "some ask" // offered; some ops or commands ask, the rest run at once
)

// MCPServeGateDocs says, for every tool the chat has, what an outside agent
// gets through termocode's MCP server. It is computed from the same
// allow-list table and gates the server uses, so the docs cannot drift.
func MCPServeGateDocs() map[string]string {
	out := map[string]string{}
	for _, t := range mcpServeCatalog() {
		out[t.Name] = mcpServeGateWord(t)
	}
	return out
}

// mcpServeGateWord classifies one exposed tool by asking its gate with
// each value of its enum arguments (the table may have narrowed them).
func mcpServeGateWord(t ai.Tool) string {
	switch t.Name {
	case "execute_action":
		return MCPServeSome // each palette command has its own gate (mcpServeActionAllowed, then the policy map)
	case "http_send":
		return MCPServeSome // GET / HEAD / OPTIONS to loopback run at once; the rest asks (httpAIGate)
	}
	if t.Gate == nil {
		if t.Mutating {
			return MCPServeAsks
		}
		return MCPServeYes
	}
	keys := make([]string, 0, len(t.Params))
	for k := range t.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	asks, runs := 0, 0
	count := func(args map[string]any) {
		if mut, _ := t.Gate(args); mut {
			asks++
		} else {
			runs++
		}
	}
	for _, k := range keys {
		enum := aiParamEnum(t.Params[k])
		if len(enum) == 0 {
			continue
		}
		for _, v := range enum {
			count(map[string]any{k: v})
		}
		break
	}
	if asks+runs == 0 {
		count(map[string]any{})
	}
	switch {
	case asks == 0:
		return MCPServeYes
	case runs == 0:
		return MCPServeAsks
	}
	return MCPServeSome
}
