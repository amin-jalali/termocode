package app

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The profile step form's subscription CLIs (CS-5): the form shows
// whether Claude Code / Codex is installed and logged in, checked in the
// background with each CLI's own status command. Install and Log in open
// a terminal tab: Log in runs the CLI's own login (termocode never sees
// the credentials); Install types the install line for the user to run.
// The form is parked meanwhile and comes back from its toast.

// aiCLICheck checks one CLI (a var for tests: they never run a real CLI).
var aiCLICheck = clistep.Check

// aiCLIOpenTerminal opens a terminal tab in the workspace and types line
// (run: with Enter). A var for tests.
var aiCLIOpenTerminal = func(m *Model, line string, run bool) error {
	if m.nvim == nil {
		return errors.New("no terminal")
	}
	if !m.termOpen && len(m.hiddenTerminalTabs) > 0 {
		m.toggleTerminalPanel()
	}
	m.newTerminalTab()
	t := m.activeTerminalTab()
	if t == nil {
		return errors.New("no terminal tab")
	}
	if run {
		line += "\n"
	}
	if err := m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		local ok, chan = pcall(function() return vim.bo[buf].channel end)
		if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, %q) end
	`, t.BufID, line)); err != nil {
		return err
	}
	m.showPanelTab(panelKindTerminal)
	m.applyLayout()
	m.resizeTerminalSplit()
	return nil
}

// aiCLIStatusMsg carries the CLI check.
type aiCLIStatusMsg struct {
	states map[string]profileform.CLIState
}

// aiProfileFormBackMsg: the parked form's toast was clicked.
type aiProfileFormBackMsg struct{}

func aiCLIFormState(s clistep.Status) profileform.CLIState {
	switch s {
	case clistep.StatusReady:
		return profileform.CLIReady
	case clistep.StatusMissing:
		return profileform.CLIMissing
	case clistep.StatusLoggedOut:
		return profileform.CLILoggedOut
	}
	return profileform.CLIUnknown
}

// aiCLICheckCmd checks every CLI off the Update goroutine.
func aiCLICheckCmd() tea.Cmd {
	return func() tea.Msg {
		out := map[string]profileform.CLIState{}
		for _, c := range ai.CLIs {
			out[c] = aiCLIFormState(aiCLICheck(context.Background(), c, nil))
		}
		return aiCLIStatusMsg{states: out}
	}
}

// onAICLIStatus keeps the check and shows it in an open (or parked) form.
func (m *Model) onAICLIStatus(msg aiCLIStatusMsg) tea.Cmd {
	m.aiS().cliStates = msg.states
	if m.profileFormOpen || m.profileFormParked {
		m.profileForm.SetCLIs(msg.states)
	}
	return nil
}

// onProfileFormCLIAction is the form's Install / Log in pill.
func (m *Model) onProfileFormCLIAction(msg profileform.CLIActionMsg) tea.Cmd {
	title := ai.CLITitle(msg.CLI)
	line, verb := clistep.InstallLine(msg.CLI), "Install "+title
	if msg.Login {
		line, verb = clistep.LoginLine(msg.CLI), "Log in to "+title
	}
	if line == "" {
		return nil
	}
	if err := aiCLIOpenTerminal(m, line, msg.Login); err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, verb+" in a terminal", "Run: "+line)
		return c
	}
	m.profileFormOpen, m.profileFormParked = false, true
	detail := "The terminal runs " + line + ". Click here to go back to the profile form."
	if !msg.Login {
		detail = "Press Enter in the terminal to run " + line + ". Click here to go back to the profile form."
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushClick(toast.Info, verb, detail, aiProfileFormBackMsg{})
	return c
}

// onAIProfileFormBack shows the parked form again and checks the CLIs
// once more (the login may be done now).
func (m *Model) onAIProfileFormBack() tea.Cmd {
	if !m.profileFormParked {
		return nil
	}
	m.profileFormParked, m.profileFormOpen = false, true
	m.profileForm.SetSize(m.w, m.h)
	return aiCLICheckCmd()
}
