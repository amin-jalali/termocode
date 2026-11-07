package agentlog

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// maxImportMessages is the most chat messages an import keeps (the
// newest): a long session would make a chat too big to send again.
const maxImportMessages = 400

// ImportConversation turns a parsed session into a new termocode AI chat
// (CMP-87): the prompts and the answers in order (answers in a row are
// joined), each tool call as one display line ("⚙ Tool args", "✘" when
// its result failed) and no tool results (they hold file contents). The
// text is redacted again on the way in. agent is the CLI's title. The
// chat is not saved here.
func ImportConversation(s Session, agent string) ai.Conversation {
	var msgs []ai.StoredMessage
	lastTool := -1 // index in msgs of the newest tool line
	for _, t := range s.Turns {
		switch t.Kind {
		case User, Assistant:
			text := strings.TrimSpace(redact(t.Text))
			if text == "" {
				continue
			}
			role := ai.StoredUser
			if t.Kind == Assistant {
				role = ai.StoredAssistant
			}
			if n := len(msgs); role == ai.StoredAssistant && n > 0 && msgs[n-1].Role == ai.StoredAssistant {
				msgs[n-1].Text += "\n\n" + text
				continue
			}
			msgs = append(msgs, ai.StoredMessage{Role: role, Text: text})
		case ToolCall:
			line := "⚙ " + strings.TrimSpace(t.Tool)
			if args := strings.TrimSpace(redact(t.Args)); args != "" {
				line += " " + args
			}
			msgs = append(msgs, ai.StoredMessage{Role: ai.StoredTool, Text: line})
			lastTool = len(msgs) - 1
		case ToolResult:
			if t.Failed && lastTool >= 0 && !strings.HasSuffix(msgs[lastTool].Text, " ✘") {
				msgs[lastTool].Text += " ✘"
			}
		}
	}
	c := ai.NewConversation()
	if len(msgs) == 0 {
		return c
	}
	if cut := len(msgs) - maxImportMessages; cut > 0 {
		msgs = append([]ai.StoredMessage{{Role: ai.StoredNote,
			Text: "⋯ " + strconv.Itoa(cut) + " earlier messages were not imported"}}, msgs[cut:]...)
	}
	c.Messages = msgs
	c.Title = ai.DeriveTitle(msgs)
	n := len(msgs)
	if msgs[0].Role == ai.StoredNote {
		n--
	}
	c.ImportedFrom = &ai.ImportInfo{CLI: string(s.CLI), Agent: agent, Session: s.ID, Messages: n}
	return c
}
