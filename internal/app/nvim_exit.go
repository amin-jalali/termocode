package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/toast"
)

// When the embedded nvim stops on its own (a crash, the OOM killer, a
// kill), the app asks Restart / Quit instead of running on with a dead
// editor. Restart starts a fresh nvim and runs the normal startup on it
// (nvim.ReadyMsg): the session restore reopens the tabs, and the hot-exit
// check finds this run's backups (our own pid counts as finished, see
// collectHotBackups) and offers the unsaved text back. A normal quit
// closes nvim first, so it never shows the question (nvim.ExitedMsg is
// not sent after Close).

// onNvimExited handles the end of the nvim process.
func (m *Model) onNvimExited(msg nvim.ExitedMsg) tea.Cmd {
	if m.nvim == nil || msg.Client != m.nvim {
		return nil // an old client, already replaced or closed
	}
	m.nvimDead = true
	m.nvimAttached = false
	if msg.Err != nil {
		recordError("nvim stopped: " + msg.Err.Error())
	}
	// Reap the process. Not in Update: Close may wait for it.
	go func(c *nvim.Client) { _ = c.Close() }(m.nvim)
	m.promptNvimExited()
	return nil
}

// promptNvimExited shows the "Editor process stopped" question.
func (m *Model) promptNvimExited() {
	m.confirm = confirm.New("Editor process stopped",
		"The nvim process behind the editor ended. Restart it to reopen your files; unsaved text can be restored from the backup.",
		[]confirm.Button{
			{ID: "restart", Title: "Restart", Style: confirm.StylePrimary},
			{ID: "quit", Title: "Quit", Style: confirm.StyleDestructive},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindNvimExited
}

// onNvimExitedConfirm handles Restart / Quit.
func (m *Model) onNvimExitedConfirm(id string) tea.Cmd {
	if id == "quit" {
		// nvim is closed already. The hot-exit backup stays: the next
		// start offers the unsaved text back.
		return tea.Quit
	}
	return m.restartNvim()
}

// restartNvim starts a new nvim in place of the dead one.
func (m *Model) restartNvim() tea.Cmd {
	client, err := nvim.New()
	if err != nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Restart failed", err.Error())
		m.promptNvimExited()
		return cmd
	}
	m.nvim = client
	m.nvimDead = false
	m.editor = editor.New(client)
	m.syncTermPalette()
	m.subscribeNotify()
	m.applyLayout()
	return tea.Batch(m.attachCmd(), m.nvim.Next())
}
