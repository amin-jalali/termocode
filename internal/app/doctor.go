package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/setup"
)

// runDoctorCmd backs "Help: Run Doctor". It runs the read-only `setup`
// checks off the UI goroutine (fc-list and `nvim --version` can take a
// moment) and shows the report in the preview overlay via PreviewMsg.
func runDoctorCmd() tea.Cmd {
	return func() tea.Msg {
		return PreviewMsg{Title: " Doctor ", Body: setup.Report(setup.Doctor())}
	}
}
