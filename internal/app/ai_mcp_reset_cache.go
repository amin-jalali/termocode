package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// resetMCPToolLists is "AI: MCP: Reset Tool Lists" (RT-14): every saved
// tool list is forgotten, so each server lists its tools again on next
// use (a server that changed its tools without saying so).
func (m *Model) resetMCPToolLists() tea.Cmd {
	m.aiMCP().Reload()
	n := m.aiMCP().ResetToolCache("")
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "MCP: forgot the saved tool lists of "+itoa(n)+" server"+plural(n)+"; each lists its tools again on next use")
	return c
}
