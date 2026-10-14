package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// toastPlace is one toast card where the view draws it (toastPlaces).
type toastPlace struct {
	card    toast.Card
	x, y, w int
	compact bool
}

// toastClickAt is the click message of the clickable toast at (x, y)
// (toast.PushClick), and its ID; nil when there is none.
func (m Model) toastClickAt(x, y int) (tea.Msg, int) {
	for _, p := range m.toastPlaces() {
		if p.card.Click != nil && x >= p.x && x < p.x+p.w && y >= p.y && y < p.y+len(p.card.Lines) {
			return p.card.Click, p.card.ID
		}
	}
	return nil, 0
}

// clickToast runs a left click on a clickable toast: the toast goes away
// and its message is sent. ok is false when no such toast is there.
func (m *Model) clickToast(msg tea.MouseMsg) (tea.Cmd, bool) {
	if msg.Type != tea.MouseLeft || msg.Action == tea.MouseActionRelease {
		return nil, false
	}
	click, id := m.toastClickAt(msg.X, msg.Y)
	if click == nil {
		return nil, false
	}
	m.toast = m.toast.DismissID(id)
	return func() tea.Msg { return click }, true
}
