package app

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Crash safety (STAB-6): a panic in a background goroutine or command is
// recovered by internal/crash, which writes a report and sends a notice;
// the UI shows it as a toast and goes on.

// crashUI is shared by the Model copies.
type crashUI struct {
	stop chan struct{}
	once sync.Once
}

func newCrashUI() *crashUI {
	return &crashUI{stop: make(chan struct{})}
}

// close ends the notice listener (Model.Close).
func (c *crashUI) close() {
	if c == nil {
		return
	}
	c.once.Do(func() { close(c.stop) })
}

// crashNoticeMsg is one recovered panic.
type crashNoticeMsg struct{ n crash.Notice }

// waitCmd waits for the next recovered panic.
func (c *crashUI) waitCmd() tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case n := <-crash.Notices():
			return crashNoticeMsg{n}
		case <-c.stop:
			return nil
		}
	}
}

// handleCrashMsg shows a recovered panic as an error toast.
func (m *Model) handleCrashMsg(msg tea.Msg) (tea.Cmd, bool) {
	n, ok := msg.(crashNoticeMsg)
	if !ok {
		return nil, false
	}
	title := "Something went wrong in " + n.n.Where + "; your work is safe."
	detail := "Report saved."
	if n.n.Report == "" {
		detail = "The report could not be saved."
	}
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushDetail(toast.Errr, title, detail)
	return tea.Batch(cmd, m.crash.waitCmd()), true
}
