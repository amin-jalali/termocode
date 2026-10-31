package app

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agenthooks"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Agent: State Hooks… (OMX-34): put a small hook into an outside agent's
// own config so it reports working / waiting / done over the control
// socket (`termocode agent report`) instead of termocode guessing from
// its screen. The card shows the file and every line that changes, with
// Not now first; the old file is kept as <file>.termocode-backup.

const (
	pickerKindAgentHooks  pickerKindEnum  = 2907
	confirmKindAgentHooks confirmKindEnum = 2908
)

// agentHooksExe is the termocode program the hooks run (a var for tests).
var agentHooksExe = func() string {
	if exe, err := spawn.Executable(); err == nil && exe != "" {
		return exe
	}
	return "termocode"
}

// agentHooksHome is the folder of the agents' config files.
func agentHooksHome() string {
	home, _ := os.UserHomeDir()
	return home
}

func agentHookTitle(id string) string {
	if a, ok := agents.ByID(id); ok {
		return a.Title
	}
	return id
}

// openAgentHooks lists the agents with a hook format and their state.
func (m *Model) openAgentHooks() tea.Cmd {
	home := agentHooksHome()
	var items []picker.Item
	for _, id := range agenthooks.Agents() {
		p, err := agenthooks.PlanInstall(id, home, agentHooksExe())
		hint := "not installed · " + p.Path
		switch {
		case err != nil:
			hint = agentCtlShort(err.Error())
		case p.Installed:
			hint = "installed · Enter to remove"
		}
		items = append(items, picker.Item{ID: id, Title: agentHookTitle(id), Hint: hint})
	}
	m.picker = picker.NewItems(" Agent State Hooks ", items)
	m.picker.SetNote([]string{"The agent tells termocode when it works, waits for you or is done. You see the change first."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentHooks
	return nil
}

func agentCtlShort(s string) string {
	s = ai.Redact(s)
	if len(s) > 120 {
		s = s[:119] + "…"
	}
	return s
}

// onAgentHooksPicked shows the install (or remove) card.
func (m *Model) onAgentHooksPicked(id string) tea.Cmd {
	home := agentHooksHome()
	p, err := agenthooks.PlanInstall(id, home, agentHooksExe())
	if err == nil && p.Installed {
		p, err = agenthooks.PlanRemove(id, home)
	}
	if err != nil {
		return m.aiInfo(agentCtlShort(err.Error()))
	}
	if p.New == nil {
		return m.aiInfo("Nothing to change in " + p.Path)
	}
	m.aiS().agentHooksPlan = &p
	verb, title := "Install", "Install state hooks for "+agentHookTitle(id)+"?"
	if p.Remove {
		verb, title = "Remove", "Remove the state hooks of "+agentHookTitle(id)+"?"
	}
	body := "Changes " + p.Path + ":\n\n    " + strings.Join(p.Lines, "\n    ") +
		"\n\nThe old file is kept as " + p.Path + ".termocode-backup."
	if !p.Remove {
		body += "\nThe hook does nothing outside a termocode terminal."
	}
	m.confirm = confirm.Risky(title, body,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "apply", Title: verb, Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentHooks
	return nil
}

// answerAgentHooks writes the shown plan on Install / Remove.
func (m *Model) answerAgentHooks(id string) tea.Cmd {
	s := m.aiS()
	p := s.agentHooksPlan
	s.agentHooksPlan = nil
	if p == nil || id != "apply" {
		return nil
	}
	if err := agenthooks.Apply(*p); err != nil {
		return m.aiInfo("Could not change " + p.Path + ": " + agentCtlShort(err.Error()))
	}
	if p.Remove {
		return m.aiInfo("State hooks removed from " + p.Path)
	}
	return m.aiInfo("State hooks installed: start " + agentHookTitle(p.Agent) + " again in a terminal tab")
}
