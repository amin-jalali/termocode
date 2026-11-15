package app

import "github.com/amin-jalali/termocode/internal/ai"

// aiGuardWrites puts the open chat's read list on a turn's tools
// (ai_write_needs_read, default on): write_file refuses an existing file
// the chat has not read or written. Another chat (or a new one) starts
// with an empty list.
func (m *Model) aiGuardWrites(tools []ai.Tool, root string) []ai.Tool {
	s := m.aiS()
	if !s.settings.WriteNeedsReadOn() {
		return tools
	}
	if s.readSeen == nil || s.readSeenChat != s.conv.ID {
		s.readSeen, s.readSeenChat = ai.NewFileLedger(), s.conv.ID
	}
	return ai.GuardWrites(tools, m.aiWorkspaceAt(root), s.readSeen)
}
