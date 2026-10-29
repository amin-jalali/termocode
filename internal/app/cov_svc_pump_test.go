package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// covSvcRun runs a command tree like the Bubble Tea loop: every command
// on its own goroutine, every answer through Model.Update. It returns
// when nothing has answered for quiet (toast timers are not followed).
func covSvcRun(t *testing.T, m *Model, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	return covSvcRunQuiet(t, m, cmd, 400*time.Millisecond)
}

func covSvcRunQuiet(t *testing.T, m *Model, cmd tea.Cmd, quiet time.Duration) []tea.Msg {
	t.Helper()
	results := make(chan tea.Msg, 256)
	var seen []tea.Msg
	n := 0
	start := func(c tea.Cmd) {
		if c == nil {
			return
		}
		if n++; n > 400 {
			t.Fatal("command loop")
		}
		go func() { results <- c() }()
	}
	start(cmd)
	deadline := time.After(30 * time.Second)
	for {
		select {
		case got := <-results:
			switch msg := got.(type) {
			case nil, toast.TickMsg:
				continue
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
				continue
			case tea.QuitMsg:
				t.Fatal("a command quit the app")
			}
			seen = append(seen, got)
			next, c := m.Update(got)
			*m = next.(Model)
			start(c)
		case <-time.After(quiet):
			return seen
		case <-deadline:
			t.Fatal("commands did not settle")
		}
	}
}
