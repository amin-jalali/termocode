package webproto

import "time"

// AI: Side Question… from the page (WEBP-9): a quick question to the
// quick model beside the main chat, even while it answers. The laptop
// answers from a read-only snapshot of its open chat, with no tools; the
// question and answer stay in the side history (the laptop's side card
// shows the same) and never go into the main chat. It spends the user's
// tokens, like chat.send, so only a controller asks.

// OpChatSide is the side question op; its args are ChatSendArgs (the
// text, and the chat it is beside, if the page names one).
const OpChatSide = "chat.side"

// ChatSide is the result of chat.side: the answer, redacted.
type ChatSide struct {
	Answer string `json:"answer"`
}

var opSideRows = []Policy{
	{Op: OpChatSide, New: newOf[ChatSendArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 72 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opSideRows...); return true }()

func init() {
	registerResult(OpChatSide, ChatSide{})
}
