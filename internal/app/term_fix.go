package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── "✦ Fix?" chip (U9b, competitive plan T2) ─────────────────────────────
//
// When a command in the terminal tab on show ends with a non-zero exit
// code (shell integration, term_integration.go), the tab gets a small
// "✦ Fix?" chip. Nothing is sent before the user acts: a click on the
// chip, Alt+\ in the terminal, or "Terminal: Fix Last Command with AI"
// sends the command, its exit code, its folder and the last output lines
// (redacted, size-capped) to the AI chat. The chip is hidden while AI is
// not set up, and a new command clears it.

// termFixChipW is the cell width of the chip label.
const termFixChipW = 6

// termFixChipLabel is "✦ Fix?" in the current icon mode.
func termFixChipLabel() string { return theme.GlyphAI.String() + " Fix?" }

// termFixLines is how many output lines go with the prompt.
const termFixLines = 60

// termFixMaxBytes caps the output sent (after redaction).
const termFixMaxBytes = 8 * 1024

// aiChipOn: AI is set up and on screen (not zen mode). Render-safe: it
// never creates the AI state.
func (m Model) aiChipOn() bool {
	s := m.ai
	return s != nil && s.provider != nil && s.provider.Configured() && !m.zenMode
}

// termTabAt returns the terminal tab of a bar entry.
func (m Model) termTabAt(e panelBarEntry) (terminalTab, bool) {
	if e.Kind != panelKindTerminal || e.Index < 0 || e.Index >= len(m.terminalTabs) {
		return terminalTab{}, false
	}
	return m.terminalTabs[e.Index], true
}

// termTabHasChip: the entry is a terminal tab whose chip shows.
func (m Model) termTabHasChip(e panelBarEntry) bool {
	t, ok := m.termTabAt(e)
	return ok && t.FixChip && m.aiChipOn()
}

// termFixWithAI sends the failed command of terminal tab idx to the AI
// chat (the chip click, Alt+\, the palette).
func (m *Model) termFixWithAI(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.terminalTabs) {
		return nil
	}
	t := &m.terminalTabs[idx]
	if t.LastCmd == "" || t.LastExit <= 0 {
		return m.aiInfo("No failed command in this terminal")
	}
	if c, ok := m.aiReady(); !ok {
		return c
	}
	out, _ := m.termTail(t.BufID, termFixLines)
	t.FixChip = false
	s := m.aiS()
	s.attachments = append(s.attachments, aiAttachment{
		Label: fmt.Sprintf("terminal output (last %d lines)", termFixLines),
		Text:  termFixRedact(out),
	})
	m.openAIPanel(true)
	return m.sendAIChat(termFixPrompt(t.LastCmd, t.LastExit, t.Cwd))
}

// termFixPrompt is the chat message: the command, its exit code and
// folder, and what to answer. Pure for tests.
func termFixPrompt(cmd string, exit int, cwd string) string {
	cmd = ai.Redact(cmd)
	var b strings.Builder
	fmt.Fprintf(&b, "This command failed in my terminal with exit code %d:\n```\n%s\n```\n", exit, cmd)
	if cwd != "" {
		fmt.Fprintf(&b, "It ran in %s.\n", cwd)
	}
	b.WriteString("Propose one fixed command and explain the fix in one line. The terminal output is attached.")
	return b.String()
}

// termFixRedact redacts secrets in the output and keeps its end within
// termFixMaxBytes.
func termFixRedact(out string) string {
	out = ai.Redact(out)
	if len(out) > termFixMaxBytes {
		cut := len(out) - termFixMaxBytes
		for cut < len(out) && out[cut]&0xC0 == 0x80 { // not inside a UTF-8 rune
			cut++
		}
		out = "… [cut]\n" + out[cut:]
	}
	return out
}

// termTail is the last n lines of terminal buffer buf, without the
// blank rows below the prompt.
func (m *Model) termTail(buf, n int) (string, error) {
	if m.nvim == nil {
		return "", nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(agentTailLua, &raw, buf, n+300); err != nil {
		return "", err
	}
	return aiTermTail(strings.Split(raw, "\n"), n), nil
}

// termFixKey is Alt+\ inside the terminal: fix the active tab's failed
// command when it shows the chip. ok=false: the key does what it does
// elsewhere.
func (m *Model) termFixKey() (tea.Cmd, bool) {
	if !m.inTerminal || !m.termOpen || m.panelActive != panelKindTerminal {
		return nil, false
	}
	t := m.activeTerminalTab()
	if t == nil || !t.FixChip || !m.aiChipOn() {
		return nil, false
	}
	return m.termFixWithAI(m.terminalActiveTab), true
}

// termDemo sets the states the shots show (DevActionMsg "termdemo"): the
// first tab runs Claude Code, which waits for the user; the active tab's
// `go test` failed and shows the Fix chip.
func (m *Model) termDemo() {
	if len(m.terminalTabs) == 0 {
		return
	}
	if len(m.terminalTabs) > 1 {
		t := &m.terminalTabs[0]
		t.Agent, t.AgentState = "claude", agents.Waiting
	}
	t := m.activeTerminalTab()
	t.Integrated, t.LastCmd, t.LastExit, t.FixChip = true, "go test ./...", 1, true
}

// termFixPalette is "Terminal: Fix Last Command with AI".
func (m *Model) termFixPalette() tea.Cmd {
	if !m.termOpen || len(m.terminalTabs) == 0 {
		return m.aiInfo("No terminal is open")
	}
	return m.termFixWithAI(m.terminalActiveTab)
}
