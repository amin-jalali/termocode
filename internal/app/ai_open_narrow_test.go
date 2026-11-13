package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// SCN-8: in a 60x20 window the explorer leaves no room for the chat
// panel, so AI: Open Chat did nothing you could see. It must make room
// (hide the explorer) so the chat shows.
func TestOpenAIChatInSmallWindowMakesRoom(t *testing.T) {
	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m := m0.(Model)
	m.showExp = true
	m.applyLayout()
	if m.actionsPanelVisible() {
		t.Skip("panel fits with the explorer")
	}
	m.openAIPanel(true)
	if !m.actionsPanelVisible() {
		t.Fatalf("AI: Open Chat at 60x20 shows no chat panel (showExp=%v, width=%d)", m.showExp, m.actionsColumnWidth())
	}
}
