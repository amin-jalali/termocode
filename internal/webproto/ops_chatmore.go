package webproto

import (
	"strings"
	"time"
)

// The chat history ops (web parity, AI chat): what the laptop's AI: Rename
// Chat…, Fork Chat from Message…, Delete Chat…, Clear Chat History and
// Export Chat do, from the web chat's list and its More Actions menu.
// Rename and fork change nothing that cannot be undone; delete and clear
// can not be undone, so they wait for a laptop card (Cancel first).
// Export answers with the chat as markdown for the browser to save: the
// laptop writes no file.

// Chat history op names.
const (
	OpChatRename = "chat.rename"
	OpChatFork   = "chat.fork"
	OpChatDelete = "chat.delete"
	OpChatClear  = "chat.clear"
	OpChatExport = "chat.export"
)

// MaxChatTitle caps a chat title.
const MaxChatTitle = 200

// ChatRenameArgs gives chat ID a new title.
type ChatRenameArgs struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (a *ChatRenameArgs) validate(int) error {
	if !validName(a.ID, MaxID) {
		return invalid("bad chat id")
	}
	if strings.TrimSpace(a.Title) == "" || len(a.Title) > MaxChatTitle || hasControl(a.Title) {
		return invalid("bad title")
	}
	return nil
}

// ChatForkArgs forks the laptop's open chat at message I (its index).
type ChatForkArgs struct {
	I int `json:"i"`
}

func (a *ChatForkArgs) validate(int) error {
	if a.I < 0 {
		return invalid("bad message index")
	}
	return nil
}

// ChatExport is the result of chat.export: the chat as markdown and a
// file name for it ("ai-chat-2026-11-06.md").
type ChatExport struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

var opChatMoreRows = []Policy{
	{Op: OpChatRename, New: newOf[ChatRenameArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpChatFork, New: newOf[ChatForkArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpChatDelete, New: newOf[ChatOpenArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpChatClear, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpChatExport, New: newOf[ChatOpenArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opChatMoreRows...); return true }()

func init() {
	registerResult(OpChatFork, ChatRef{})
	registerResult(OpChatDelete, AskRef{})
	registerResult(OpChatClear, AskRef{})
	registerResult(OpChatExport, ChatExport{})
}
