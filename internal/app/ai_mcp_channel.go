package app

import (
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
)

// MCP channels (PR-37): a server can push a message by itself
// (notifications/claude/channel). Only servers named in mcp_channels may;
// the message shows in the chat as a note and a toast. It is text for you:
// it is never sent to the model on its own, so a server cannot steer the AI
// through a channel.

const aiMCPChannelMaxRunes = 2000

// aiMCPChannelMsg is one pushed message.
type aiMCPChannelMsg struct {
	server, content string
	meta            map[string]string
}

// mcpChannelCh carries pushed messages from the connection goroutines to
// the UI loop. A full queue drops the newest: a noisy server cannot stall.
var mcpChannelCh = make(chan aiMCPChannelMsg, 32)

// mcpChannelAllowed is the mcp_channels check, read at every message.
func mcpChannelAllowed(server string) bool {
	return slices.Contains(mcp.LoadSettings(mcpConfigPath()).Channels, server)
}

// mcpChannelPush is the Manager's Channel hook.
func mcpChannelPush(server, content string, meta map[string]string) {
	select {
	case mcpChannelCh <- aiMCPChannelMsg{server: server, content: content, meta: meta}:
	default:
	}
}

// mcpChannelWaitCmd waits for the next pushed message (the Init command,
// and again after each one). It ends when the Model closes (the crash
// listener's stop is the Model's shared "closed" signal), so a closed
// Model leaves no goroutine that would take the next Model's messages.
func (m *Model) mcpChannelWaitCmd() tea.Cmd {
	var stop <-chan struct{}
	if m.crash != nil {
		stop = m.crash.stop
	}
	return func() tea.Msg {
		select {
		case msg := <-mcpChannelCh:
			return msg
		case <-stop:
			return nil
		}
	}
}

// onAIMCPChannel notes the message in the chat and toasts it.
func (m *Model) onAIMCPChannel(msg aiMCPChannelMsg) tea.Cmd {
	text := ai.Redact(ai.RedactSecrets(aiCleanText(msg.content)))
	if r := []rune(text); len(r) > aiMCPChannelMaxRunes {
		text = string(r[:aiMCPChannelMaxRunes]) + "…"
	}
	var meta []string
	for k, v := range msg.meta {
		meta = append(meta, ai.RedactSecrets(aiCleanText(k))+"="+ai.RedactSecrets(aiCleanText(v)))
	}
	sort.Strings(meta)
	head := "⌁ " + ai.Redact(aiCleanText(msg.server)) + " says"
	if len(meta) > 0 {
		head += " (" + strings.Join(meta, ", ") + ")"
	}
	m.aiNote(head + ":\n" + text)
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, ai.Redact(aiCleanText(msg.server))+" sent a message", firstLineOf(text))
	return tea.Batch(c, m.mcpChannelWaitCmd())
}
