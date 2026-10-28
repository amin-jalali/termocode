package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// The profile step form's sign-in AIs: whether Claude Code / Codex is
// installed and signed in, checked in the background with each CLI's own
// status command. The form's model picker tags a CLI that is not ready
// ("needs sign-in"), and a step on it shows Fix, which opens the Add AI
// card at that AI (ai_profile_form.go); the form never opens a terminal.

// aiCLICheck checks one CLI (a var for tests: they never run a real CLI).
var aiCLICheck = clistep.Check

// aiCLIStatusMsg carries the CLI check.
type aiCLIStatusMsg struct {
	states map[string]profileform.CLIState
}

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

// onAICLIStatus keeps the check and shows it in an open form.
func (m *Model) onAICLIStatus(msg aiCLIStatusMsg) tea.Cmd {
	m.aiS().cliStates = msg.states
	if m.profileFormOpen {
		m.profileForm.SetAIs(m.aiFormGroups(), msg.states)
	}
	return nil
}
