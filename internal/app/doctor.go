package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/setup"
	"github.com/amin-jalali/termocode/internal/theme"
)

// runDoctorCmd backs "Help: Run Doctor". It runs the read-only `setup`
// checks off the UI goroutine (fc-list and `nvim --version` can take a
// moment) and shows the report in the preview overlay via PreviewMsg.
func runDoctorCmd() tea.Cmd {
	return func() tea.Msg {
		checks := setup.Doctor()
		// Replace the environment-only graphics row with the live probe.
		for i, c := range checks {
			switch c.Name {
			case "graphics (images)":
				checks[i] = setup.GraphicsCheck(termCaps, true)
				checks[i].Detail += " · using: " + imageProtocol().String()
			case "terminal":
				caps := termCaps
				checks[i] = setup.TerminalCheck(os.Getenv, &caps)
			case "Nerd Font":
				// Doctor() just ran a fresh fc-list and refreshed the cache.
				checks[i] = setup.NerdFontCheck(setup.DetectNerdFont(false), iconModeName(theme.CurrentIconMode))
			}
		}
		return PreviewMsg{Title: " Doctor ", Body: setup.Report(checks)}
	}
}
