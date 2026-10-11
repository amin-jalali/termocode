package testkit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/app"
)

// Restart quits the app and starts a new one in the same sandbox (same
// HOME, config, session files and workspace), as a user who closes and
// relaunches termocode. Messages still in flight from the old app are
// dropped.
func (d *Driver) Restart() string {
	d.T.Helper()
	d.history = append(d.history, "restart")
	close(d.done)
	if m, ok := d.model.(app.Model); ok {
		_ = m.Close()
	}
	d.msgs = make(chan tea.Msg, 4096)
	d.done = make(chan struct{})
	d.ready, d.quit = false, false

	var initCmd tea.Cmd
	if !d.call("New+Init", func() { d.model = app.New(); initCmd = d.model.Init() }) {
		d.T.FailNow()
	}
	d.run(initCmd)
	d.Send(tea.WindowSizeMsg{Width: d.W, Height: d.H})
	if d.opt.Nvim {
		d.WaitFor(func() bool { return d.ready }, 15*time.Second)
		if !d.ready {
			d.fail("nvim did not become ready within 15s after restart")
		}
	}
	return d.frame
}
