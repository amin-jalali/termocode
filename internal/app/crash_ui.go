package app

import (
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
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

	// The last run crashed (crash.Pending at start): report is its file.
	// After it the unsaved work comes back without the Restore question
	// (restored files); then a card says so.
	report      string
	restored    int
	queued      bool // the card message is on its way
	cardWaiting bool // shown once no other dialog is open
}

func newCrashUI() *crashUI {
	return &crashUI{stop: make(chan struct{}), report: crash.Pending()}
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

// handleCrashMsg shows a recovered panic as an error toast, and the card
// about the last run's crash.
func (m *Model) handleCrashMsg(msg tea.Msg) (tea.Cmd, bool) {
	if _, ok := msg.(crashCardMsg); ok {
		if m.crash != nil {
			m.crash.cardWaiting = true
			m.showWaitingCrashCard()
		}
		return nil, true
	}
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

// crashCardMsg shows the card about the crash of the last run.
type crashCardMsg struct{}

// crashCardCmd queues the card about the last run's crash, once.
func (m *Model) crashCardCmd() tea.Cmd {
	c := m.crash
	if c == nil || c.report == "" || c.queued {
		return nil
	}
	c.queued = true
	return func() tea.Msg { return crashCardMsg{} }
}

// restoreAfterCrash brings the unsaved work of a crashed run back at once
// (no Restore question: the card tells about it). It reports whether it
// did; otherwise the usual question follows. Like Restore, it first
// checks the disk (hot_exit_disk.go) and the returned command tells the
// user which files changed there.
func (m *Model) restoreAfterCrash(prev string, b []hotBackup) (tea.Cmd, bool) {
	c := m.crash
	if c == nil || c.report == "" || len(b) == 0 || m.nvim == nil {
		return nil, false
	}
	b, disk := hotExitDiskCheck(b)
	if err := m.restoreHotBackups(b, false); err != nil {
		recordError("crash restore: " + err.Error())
		return nil, false
	}
	_ = os.RemoveAll(prev) // the live backup holds them again
	c.restored += len(b)
	return m.tellHotDisk(disk), true
}

// showWaitingCrashCard opens the card once no other dialog is open.
func (m *Model) showWaitingCrashCard() {
	c := m.crash
	if c == nil || !c.cardWaiting || m.confirmOpen || m.promptOpen {
		return
	}
	c.cardWaiting = false
	crash.ClearPending()
	body := "termocode stopped unexpectedly last time. A report was saved."
	if c.restored > 0 {
		body = "termocode stopped unexpectedly last time. Your unsaved work was restored."
	}
	// Close first and focused (AH-1).
	m.confirm = confirm.New("Something went wrong", body, []confirm.Button{
		{ID: "close", Title: "Close", Style: confirm.StylePrimary},
		{ID: "show", Title: "Show report"},
		{ID: "copy", Title: "Copy report path"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindCrash
}

// onCrashConfirm handles Close / Show report / Copy report path.
func (m *Model) onCrashConfirm(id string) tea.Cmd {
	path := ""
	if m.crash != nil {
		path = m.crash.report
	}
	switch id {
	case "show":
		m.openFileAtLine(path, 1)
	case "copy":
		return m.copyWithToast(path, "Copied the report path")
	}
	return nil
}

func init() {
	// Crash reports pass the same secret mask as errors.log.
	crash.SetRedactor(ai.Redact)
}
