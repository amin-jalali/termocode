package app

import tea "github.com/charmbracelet/bubbletea"

// waitWeb reads the next hub message (nil once the hub stopped). Update
// re-arms it after each web message while the hub runs.
func waitWeb(h *webHub) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-h.ch:
			return msg
		case <-h.done:
			return nil
		}
	}
}

// handleWebMsg routes the hub messages. ok=false: not ours.
func (m *Model) handleWebMsg(msg tea.Msg) (tea.Cmd, bool) {
	return nil, false
}
