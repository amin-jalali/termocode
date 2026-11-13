package session

// Paths are the files of one workspace session.
type Paths struct {
	Workspace string // absolute workspace folder
	Key       string
	Socket    string // unix socket the server listens on
	Lock      string // flock held by the live server
	Info      string // JSON with the workspace, pid and start time
	Log       string // server stderr
	Caps      string // caps of the last client that attached (ClientCaps)
	// MCPSocket is where the UI serves MCP to outside agents (M1); the
	// `termocode mcp` relay connects to it.
	MCPSocket string
	// AgentSocket is the control socket for scripts and outside agents
	// in the terminal tabs (OMX-33, internal/agentctl).
	AgentSocket string
	// WebSocket is the web hub in the UI process (ADR 0020); the web
	// gateway opens one connection per browser plus a control connection.
	WebSocket string
	// WebHTTPSocket is where the web gateway serves HTTP in SSH mode.
	WebHTTPSocket string
	// WebStatus is the running web gateway's status file (0600).
	WebStatus string
	// WebLock is held by the running web gateway (one per workspace).
	WebLock string
	// WebResume is what a gateway hands the next one across a reload
	// into a new build (0600: its port and token digests, no secret).
	WebResume string
	// WebOpen is the short-lived 0600 page that sends the browser to the
	// one-time link, so the link is never in a program's argv.
	WebOpen string
}
