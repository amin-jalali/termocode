package webproto

// The mcp data (WEB-45, web design 2.9): what the laptop's "MCP: Server
// Status" screen shows: whether termocode listens as an MCP server for
// outside agents, the agents connected now and their last calls. Read
// only: the web cannot start, stop, connect, disconnect or allow an
// agent. The delta is the whole snapshot again.

func init() {
	registerTopic("mcp", MCPSnap{}, MCPSnap{})
}

// MCP limits.
const (
	MaxMCPAgents = 64 // agents listed
	MaxMCPCalls  = 20 // last calls listed, oldest first
)

// MCPSnap is the mcp topic.
type MCPSnap struct {
	Mode      string     `json:"mode" enum:"auto|on|off"` // the mcp_server setting
	Listening bool       `json:"listening"`
	Why       string     `json:"why,omitempty"` // not listening: why, one sentence
	Agents    []MCPAgent `json:"agents"`
	Calls     []string   `json:"calls"` // "09:41 agent · tool · asked, you allowed"
}

// MCPAgent is one connected outside agent.
type MCPAgent struct {
	Name  string `json:"name"`  // "" until it said its name
	Since int64  `json:"since"` // Unix ms
	Calls int    `json:"calls"`
	Asked int    `json:"asked"` // calls that showed a card
}
