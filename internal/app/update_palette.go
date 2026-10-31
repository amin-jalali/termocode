package app

import (
	"context"
	"github.com/amin-jalali/termocode/internal/netmsg"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/update"
)

// G-UPD palette commands: Check for Updates (any time, also with
// update_check off) and Roll Back Update (only while termocode.old is
// kept).

func updatePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "update-check", Title: "termocode: Check for Updates"},
		{ID: "update-rollback", Title: "termocode: Roll Back Update"},
	}
}

// updatePaletteFilter hides Roll Back Update while there is nothing to
// roll back.
func updatePaletteFilter(items []picker.Item) []picker.Item {
	if updateCanRollback() {
		return items
	}
	out := items[:0:0]
	for _, it := range items {
		if it.ID != "update-rollback" {
			out = append(out, it)
		}
	}
	return out
}

// updateCanRollback reports whether termocode.old sits next to the
// running binary.
func updateCanRollback() bool {
	exe, err := updateDepsFor().exe()
	return err == nil && update.CanRollback(updateInstallPath(exe))
}

// updateInstallPath is exe with symlinks resolved (where Apply wrote).
// It only reads: the palette calls it on every open.
func updateInstallPath(exe string) string {
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// dispatchUpdatePalette runs the update palette ids. ok=false: not ours.
func (m *Model) dispatchUpdatePalette(id string) (tea.Cmd, bool) {
	switch id {
	case "update-check":
		return m.updateCheckNow(), true
	case "update-rollback":
		return m.openUpdateRollback(), true
	}
	return nil, false
}

// updateManualMsg is the answer of Check for Updates.
type updateManualMsg struct {
	state update.State
	err   error
}

// updateCheckNow asks GitHub at once.
func (m *Model) updateCheckNow() tea.Cmd {
	if !update.IsRelease(termocodeVersion) {
		return m.pushToastDetail(toast.Info, "This termocode is built from source ("+termocodeVersion+")",
			"Update it with git pull and go build, or install a release: "+update.InstallScript)
	}
	d := updateDepsFor()
	now := clock.Now()
	return func() tea.Msg {
		path := update.StatePath(d.getenv)
		st := update.LoadState(path)
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		rel, err := d.client.Latest(ctx)
		if err != nil {
			return updateManualMsg{state: st, err: err}
		}
		st.LastCheck, st.Latest, st.NotesURL = now, rel.Tag, rel.HTMLURL
		if st.Skipped == rel.Tag {
			st.Skipped = "" // asked for by hand: show it again
		}
		_ = update.SaveState(path, st)
		return updateManualMsg{state: st}
	}
}

// onUpdateManual shows the card or says termocode is up to date.
func (m *Model) onUpdateManual(msg updateManualMsg) tea.Cmd {
	if msg.err != nil {
		detail := msg.err.Error()
		if why, ok := netmsg.Why(msg.err); ok {
			detail = why
		}
		return m.pushToastDetail(toast.Warn, "Could not check for updates", detail)
	}
	w := m.updW()
	w.state = msg.state
	if w.state.Available(termocodeVersion) == "" {
		return m.pushToastDetail(toast.Info, "termocode "+termocodeVersion+" is up to date", "")
	}
	return m.openUpdateCard()
}

// openUpdateRollback asks before putting termocode.old back.
func (m *Model) openUpdateRollback() tea.Cmd {
	exe, err := updateDepsFor().exe()
	if err != nil || !update.CanRollback(updateInstallPath(exe)) {
		return m.pushToastDetail(toast.Info, "Nothing to roll back", "No earlier termocode is kept next to this one.")
	}
	path := updateInstallPath(exe)
	m.confirm = confirm.New("Roll back termocode?",
		"Puts back the termocode that the last update replaced ("+update.OldPath(path)+"). The current one is removed. Restart afterwards to use it.",
		[]confirm.Button{
			{ID: "cancel", Title: "Cancel"},
			{ID: "rollback", Title: "Roll back", Style: confirm.StyleDestructive},
		}).WithWideText().WithFocus(0)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindUpdateRollback
	return nil
}

// answerUpdateRollback runs the rollback card's answer.
func (m *Model) answerUpdateRollback(id string) tea.Cmd {
	if id != "rollback" {
		return nil
	}
	exe, err := updateDepsFor().exe()
	if err == nil {
		err = update.Rollback(updateInstallPath(exe))
	}
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Roll back failed: nothing was changed", err.Error())
	}
	m.updW().installed = "rollback"
	m.confirm = confirm.New("termocode is rolled back", "Restart termocode to use the earlier version. Unsaved text is kept, as on Reload Window.", []confirm.Button{
		{ID: "cancel", Title: "Later"},
		{ID: "restart", Title: "Restart", Style: confirm.StylePrimary},
	}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindUpdateRestart
	return nil
}
