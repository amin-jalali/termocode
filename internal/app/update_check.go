package app

import (
	"context"
	"os"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/update"
)

// G-UPD: termocode looks for a newer release on GitHub at most once a
// day, in the background (update_check, release builds only). A newer
// one shows the quiet status-bar chip "⬆ v0.2.0". All network errors
// are quiet: offline or rate-limited simply means no chip.

// updateCheckTimeout caps one GitHub request.
const updateCheckTimeout = 20 * time.Second

// updateDeps is what the update code reaches outside (tests swap it).
type updateDeps struct {
	client       update.Client
	getenv       func(string) string
	exe          func() (string, error)
	owner        func(string) (string, bool)
	goos, goarch string
	// headlessOK lets tests check without a terminal.
	headlessOK bool
}

var updateDepsFor = func() updateDeps {
	return updateDeps{
		client: update.Client{Version: termocodeVersion},
		getenv: os.Getenv,
		exe:    spawn.Executable,
		owner:  update.PacmanOwner,
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}
}

// updateWatch is the update state of this window.
type updateWatch struct {
	state update.State
}

// updateCheckedMsg is a finished check (or the saved state when no check
// was due).
type updateCheckedMsg struct {
	state update.State
	err   error
}

// updW returns the watch, creating it on first use.
func (m *Model) updW() *updateWatch {
	if m.upd == nil {
		m.upd = &updateWatch{}
	}
	return m.upd
}

// updateInitCmd starts the background check at launch, or nil for dev
// builds, without a terminal, or with update_check off.
func (m Model) updateInitCmd() tea.Cmd {
	d := updateDepsFor()
	if !update.IsRelease(termocodeVersion) || (headless() && !d.headlessOK) || !updateCheckOn() {
		return nil
	}
	return func() tea.Msg { return updateBackgroundCheck(d, clock.Now()) }
}

// updateBackgroundCheck loads the saved state and asks GitHub when a
// check is due. The check time is saved even when it fails, so an
// offline machine asks once a day, not on every start.
func updateBackgroundCheck(d updateDeps, now time.Time) tea.Msg {
	path := update.StatePath(d.getenv)
	st := update.LoadState(path)
	if !update.Due(st, now) {
		return updateCheckedMsg{state: st}
	}
	st.LastCheck = now
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	rel, err := d.client.Latest(ctx)
	if err == nil {
		st.Latest, st.NotesURL = rel.Tag, rel.HTMLURL
	}
	_ = update.SaveState(path, st)
	return updateCheckedMsg{state: st, err: err}
}

// handleUpdateMsg handles the update messages (any overlay may be open).
func (m *Model) handleUpdateMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case updateCheckedMsg:
		m.updW().state = msg.state
		return nil, true
	}
	return nil, false
}

// fillUpdateStatus sets the status-bar update chip.
func (m Model) fillUpdateStatus(st *statusbar.State) {
	if m.upd != nil {
		st.Update = m.upd.state.Available(termocodeVersion)
	}
}
