package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// Long MCP calls in the background (MCPX-19, RT-21): while an MCP tool
// runs, the activity line says "Ctrl+B: background". Ctrl+B (or the
// palette's "AI: Move MCP Call to Background") lets the model go on at
// once; the call keeps running, and when it ends its result shows in
// the chat and is attached to the next message.

// aiMCPBackgroundMsg is one background call's end.
type aiMCPBackgroundMsg struct{ res mcp.BackgroundResult }

// aiMCPBackgroundKey is the chat key (only taken while a call runs).
const aiMCPBackgroundKey = "ctrl+b"

// aiMCPDetach moves the running MCP calls to the background. ok=false:
// nothing runs.
func (m *Model) aiMCPDetach() (tea.Cmd, bool) {
	s := m.aiS()
	if len(s.mcpx.bg.Running()) == 0 {
		return nil, false
	}
	var cmds []tea.Cmd
	for _, ch := range s.mcpx.bg.Detach() {
		ch := ch
		cmds = append(cmds, func() tea.Msg { return aiMCPBackgroundMsg{res: <-ch} })
	}
	cmds = append(cmds, m.aiInfo("Moved to the background: the result comes into the chat when it is ready"))
	return tea.Batch(cmds...), true
}

func (m *Model) onAIMCPBackground(msg aiMCPBackgroundMsg) tea.Cmd {
	s := m.aiS()
	r := msg.res
	text := r.Text
	if r.Err != nil {
		text = "error: " + ai.Redact(r.Err.Error())
	}
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ " + r.Label + " (background) → " + aiShortResult(text)})
	if len(text) > aiMaxAttach {
		text = strings.ToValidUTF8(text[:aiMaxAttach], "") + "\n… [truncated]"
	}
	s.attachments = append(s.attachments, aiAttachment{Label: "result of " + r.Label, Text: text})
	m.persistAIState()
	return m.aiInfo(r.Label + " is done: its result goes with your next message")
}

// withBgHint marks the activity line of a tool that can move away.
func (s *aiState) withBgHint(v aiActivityView) aiActivityView {
	if v.phase == aiPhaseTool && v.secs >= 3 && len(s.mcpx.bg.Running()) > 0 {
		v.bg = true
	}
	return v
}
