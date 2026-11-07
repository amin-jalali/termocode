package webproto

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Review in chat (MOB-14): the phone reads an agent worktree's diff
// (ai.worktrees, ai.worktree_text), the user adds a short note on a line,
// and "Send N notes to agent" sends them all to the chat in one message.
// The laptop builds that message: it names the worktree's branch, each
// file and line, quotes the line from the agent's file (never from a file
// that may hold secrets) and redacts the notes. It is a chat send, so it
// needs the controller and spends tokens; merge and discard stay laptop
// cards (ai.worktree_merge, ai.worktree_discard).

// OpAIWorktreeNotes sends review notes on an agent worktree to the chat.
const OpAIWorktreeNotes = "ai.worktree_notes"

// Review note limits.
const (
	MaxReviewNotes    = 30
	MaxReviewNote     = 500 // bytes of one note's text
	MaxReviewNoteLine = 1_000_000
)

// AIWorktreeNotesArgs are the notes on one agent worktree. ChatID is the
// chat to send them to ("" = the laptop's open chat), as on chat.send.
type AIWorktreeNotesArgs struct {
	ID     string       `json:"id"`
	ChatID string       `json:"chat_id,omitempty"`
	Notes  []ReviewNote `json:"notes"`
}

// ReviewNote is one note on a line of a changed file: Side "new" is a
// line of the file after the agent, "old" one before it.
type ReviewNote struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side" enum:"new|old"`
	Text string `json:"text"`
}

func (a *AIWorktreeNotesArgs) validate(int) error {
	if !validWorktreeID(a.ID) {
		return invalid("bad worktree id")
	}
	if a.ChatID != "" && !validName(a.ChatID, MaxID) {
		return invalid("bad chat id")
	}
	if len(a.Notes) == 0 {
		return invalid("no notes")
	}
	if len(a.Notes) > MaxReviewNotes {
		return invalid("too many notes")
	}
	for _, n := range a.Notes {
		if err := n.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (n *ReviewNote) validate() error {
	if !validPath(n.Path, false) || strings.HasSuffix(n.Path, "/") {
		return invalid("bad path")
	}
	if n.Line < 1 || n.Line > MaxReviewNoteLine {
		return invalid("bad line")
	}
	if n.Side != "new" && n.Side != "old" {
		return invalid("bad side")
	}
	if strings.TrimSpace(n.Text) == "" || len(n.Text) > MaxReviewNote || !utf8.ValidString(n.Text) {
		return invalid("bad note")
	}
	for _, r := range n.Text {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return invalid("bad note")
		}
	}
	return nil
}

var opReviewRows = []Policy{
	{Op: OpAIWorktreeNotes, New: newOf[AIWorktreeNotesArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 24 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opReviewRows...); return true }()

func init() {
	registerResult(OpAIWorktreeNotes, ChatRef{})
}
