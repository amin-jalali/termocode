package app

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/update"
)

// G-UPD: the in-app update of a binary the user owns. Update downloads
// and checks the release in the background (update.Client.Prepare), the
// confirm card asks (Cancel first and focused), Install swaps the binary
// (termocode.old stays for one rollback), and Restart re-execs in place.

// updatePrepareTimeout caps the download and the checks.
const updatePrepareTimeout = 10 * time.Minute

// updateToastKey is the keyed progress toast of an update.
const updateToastKey = "update"

// updatePreparedMsg is a finished download and check.
type updatePreparedMsg struct {
	staged update.Staged
	err    error
}

// updateRestart re-execs termocode (Reload Window: unsaved text is kept).
// Tests swap it.
var updateRestart = func(m *Model) tea.Cmd { return m.reloadWindow() }

// updateStart downloads and checks the newest release off the main loop.
func (m *Model) updateStart() tea.Cmd {
	w := m.updW()
	if w.busy {
		return m.pushToastDetail(toast.Info, "An update is already downloading", "")
	}
	in := w.install
	if in.Method != update.MethodSelf {
		return m.openUpdateCard()
	}
	w.busy = true
	d := updateDepsFor()
	var c tea.Cmd
	m.toast, c = m.toast.PushKeyed(updateToastKey, toast.Info, "Downloading termocode "+w.state.Available(termocodeVersion)+"…", "then checking it with checksums.txt")
	exe := in.Path
	return tea.Batch(c, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updatePrepareTimeout)
		defer cancel()
		rel, err := d.client.Latest(ctx)
		if err != nil {
			return updatePreparedMsg{err: err}
		}
		st, err := d.client.Prepare(ctx, rel, exe, d.goos, d.goarch)
		return updatePreparedMsg{staged: st, err: err}
	})
}

// onUpdatePrepared shows the confirm card, or why the update stopped.
func (m *Model) onUpdatePrepared(msg updatePreparedMsg) tea.Cmd {
	w := m.updW()
	w.busy = false
	m.toast = m.toast.Dismiss(updateToastKey)
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Update failed: nothing was changed", msg.err.Error())
	}
	st := msg.staged
	w.staged = &st
	body := fmt.Sprintf("%s → %s\n\nInstalls to: %s\nDownload: %s · verified with checksums.txt\n\nThe current binary is kept as %s (termocode: Roll Back Update).",
		termocodeVersion, st.Tag, st.Exe, humanBytes(st.Size), "termocode.old")
	m.confirm = confirm.New("Install termocode "+st.Tag+"?", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "install", Title: "Install", Style: confirm.StylePrimary},
	}).WithWideText().WithFocus(0)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindUpdateInstall
	return nil
}

// answerUpdateInstall runs the confirm card's answer.
func (m *Model) answerUpdateInstall(id string) tea.Cmd {
	w := m.updW()
	st := w.staged
	w.staged = nil
	if st == nil {
		return nil
	}
	if id != "install" {
		st.Discard()
		return nil
	}
	if err := st.Apply(); err != nil {
		return m.pushToastDetail(toast.Errr, "Update failed: nothing was changed", err.Error())
	}
	w.installed = st.Tag
	m.openUpdateRestartCard(st.Tag)
	return nil
}

// openUpdateRestartCard asks to restart into tag (Later first). While
// an AI turn runs it says Restart waits for the turn (SCN-32).
func (m *Model) openUpdateRestartCard(tag string) {
	body := "Restart termocode to use " + tag + ". Unsaved text is kept, as on Reload Window."
	if os.Getenv(session.EnvSession) != "" {
		body += "\n\nThis window runs in a background session: Restart starts the new version in it. The small session process keeps the old version until the session ends; it only passes keys and the screen through."
	}
	if updateAIBusy(m) {
		body += "\n\nAn AI turn is running: Restart waits until it ends, so the turn is not cut off."
	}
	m.confirm = confirm.New("termocode "+tag+" is installed", body, []confirm.Button{
		{ID: "cancel", Title: "Later"},
		{ID: "restart", Title: "Restart", Style: confirm.StylePrimary},
	}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindUpdateRestart
}

// updateAIBusy: an AI chat turn runs in this window.
func updateAIBusy(m *Model) bool { return m.ai != nil && m.ai.streaming }

// updateRestartAsked runs Restart: now, or after the running AI turn.
func (m *Model) updateRestartAsked() tea.Cmd {
	if updateAIBusy(m) {
		m.updW().restartAfterAI = true
		return m.pushToastDetail(toast.Info, "Restarts when the AI turn ends", "termocode then starts the new version")
	}
	return updateRestart(m)
}

// updateRestartAfterAI restarts when Restart waited for an AI turn and
// no turn runs now (a queued run that started keeps it waiting).
// ok=false: no restart is due.
func (m *Model) updateRestartAfterAI() (tea.Cmd, bool) {
	w := m.upd
	if w == nil || !w.restartAfterAI || updateAIBusy(m) {
		return nil, false
	}
	w.restartAfterAI = false
	return updateRestart(m), true
}

// updateCardClosed runs on Esc: a staged download is thrown away.
func (m *Model) updateCardClosed() {
	if m.confirmKind == confirmKindUpdateInstall && m.upd != nil && m.upd.staged != nil {
		m.upd.staged.Discard()
		m.upd.staged = nil
	}
}
