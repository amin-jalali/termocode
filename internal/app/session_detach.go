package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/toast"
)

// detachSession ("Session: Detach") lets this terminal go and keeps the
// persistent session running: the session server sees DetachSeq in our
// output, drops the client and tells it how to come back. Outside a
// session there is nothing to detach from.
func (m *Model) detachSession() tea.Cmd {
	if os.Getenv(session.EnvSession) == "" {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Not in a session",
			"Detach works when termocode runs in a persistent session (persistent_session)")
		return c
	}
	writeTerminal(session.DetachSeq)
	return nil
}
