package webproto

import "time"

// AI: Side Question… from the page (WEBP-9): a quick question to the
// quick model beside the main chat, even while it answers. The laptop
// answers from a read-only snapshot of its open chat, with no tools; the
// question and answer stay in the side history (the laptop's side card
// shows the same) and never go into the main chat. It spends the user's
// tokens, like chat.send, so only a controller asks.

// AI: MCP Prompts and Resources… (WEBP-9): ai.mcp_items lists the
// connected MCP servers' prompts and resources as the text the chat takes
// ("/server.prompt ", "@server:uri "); the page puts a pick in its chat
// draft and sends nothing. Listing may start the servers, so only a
// controller asks.

// OpAIMCPItems lists the MCP prompts and resources.
const OpAIMCPItems = "ai.mcp_items"

// MaxMCPItems caps the list.
const MaxMCPItems = 500

// MCPItem is one prompt or resource.
type MCPItem struct {
	Text  string `json:"text"`  // what goes into the chat draft
	Title string `json:"title"` // the row's label
	Hint  string `json:"hint,omitempty"`
}

// MCPItems is the result of ai.mcp_items, sorted by text.
type MCPItems struct {
	Items []MCPItem `json:"items"`
}

// OpChatSide is the side question op; its args are ChatSendArgs (the
// text, and the chat it is beside, if the page names one).
const OpChatSide = "chat.side"

// ChatSide is the result of chat.side: the answer, redacted.
type ChatSide struct {
	Answer string `json:"answer"`
}

var opSideRows = []Policy{
	{Op: OpAIMCPItems, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpChatSide, New: newOf[ChatSendArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 72 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpChatHome, New: newOf[ChatHomeArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 72 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2}, // MOB-16 (ops_chathome.go)
}

var _ = func() bool { opTable = append(opTable, opSideRows...); return true }()

func init() {
	registerResult(OpChatSide, ChatSide{})
	registerResult(OpAIMCPItems, MCPItems{})
}
