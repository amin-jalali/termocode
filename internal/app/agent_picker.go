package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Agent: Start… (AGX-1, plan 2.4) ─────────────────────────────────────
//
// One list of every agent: INSTALLED (programs on PATH or in termocode's
// tools folder, with the sign-in state where the CLI's own status command
// tells it), BUILT-IN (termocode's own AI profiles) and NOT INSTALLED
// (Enter installs where a package manager can). The Agent: Start <Title>
// rows of the palette stay as they are.

const (
	pickerKindAgentPicker pickerKindEnum = 3700
	agentPickerPaletteID                 = "agent-start"
	agentPickAgentPfx                    = "agent:"
	agentPickProfilePfx                  = "profile:"
	agentPickerWidth                     = 76 // room for the hints; still fits the window
	agentPickerNote                      = "Enter starts it in a new terminal tab; a profile opens the AI chat."
)

// agentPickProfile is one BUILT-IN row.
type agentPickProfile struct {
	name  string
	steps int
}

// agentPickInput is what the rows are built from (pure: tests and the
// perf guard fill it by hand).
type agentPickInput struct {
	agents    []agents.Agent
	installed func(agents.Agent) bool
	cli       map[string]profileform.CLIState // the last CLI check (nil: not checked)
	profiles  []agentPickProfile
	// installing are the lspinstall tools being installed now.
	installing map[string]bool
	// accounts are the saved account labels per agent (AGX-13).
	accounts map[string][]string
}

// agentPickStart starts an installed agent (a var for tests).
var agentPickStart = func(m *Model, a agents.Agent) tea.Cmd { return m.startAgentIn(a, m.agentPickDir, "") }

// agentInstalled says whether a's program can be started.
func agentInstalled(a agents.Agent) bool {
	_, err := agentLookPath(a.Bin)
	return err == nil
}

// agentSignInState is the CLI check of agent id (CLIUnknown when
// termocode cannot check that CLI or has not yet).
func agentSignInState(cli map[string]profileform.CLIState, id string) profileform.CLIState {
	if id != ai.CLIClaude && id != ai.CLICodex {
		return profileform.CLIUnknown
	}
	return cli[id]
}

// agentPickerItems are the rows in three groups.
func agentPickerItems(in agentPickInput) []picker.Item {
	const gIn, gBuilt, gNot = "INSTALLED", "BUILT-IN", "NOT INSTALLED"
	on, off := theme.GlyphRadioOn.String(), theme.GlyphRadioOff.String()
	var have, missing []picker.Item
	for _, a := range in.agents {
		id := agentPickAgentPfx + a.ID
		if in.installed != nil && in.installed(a) {
			mark, hint := off, a.Bin
			switch agentSignInState(in.cli, a.ID) {
			case profileform.CLIReady:
				mark, hint = on, a.Bin+" · signed in"
			case profileform.CLILoggedOut:
				hint = a.Bin + " · Enter to sign in"
			}
			have = append(have, picker.Item{ID: id, Title: mark + " " + a.Title, Hint: hint, Group: gIn})
			for _, l := range in.accounts[a.ID] { // AGX-13 (agent_accounts.go)
				have = append(have, picker.Item{ID: id + "@" + l, Title: "    as " + l, Hint: a.ConfigEnv + " · own folder", Group: gIn})
			}
			continue
		}
		hint := a.Bin + " · manual install"
		if t, ok := lspinstall.Lookup(a.Tool); ok && a.Tool != "" {
			hint = a.Bin + " · Enter to install (" + t.InstallKind.String() + ")"
			if in.installing[a.Tool] {
				hint = a.Bin + " · installing…"
			}
		}
		missing = append(missing, picker.Item{ID: id, Title: "  " + a.Title, Hint: hint, Group: gNot})
	}
	out := make([]picker.Item, 0, len(in.agents)+len(in.profiles)+3)
	if len(have) > 0 {
		out = append(out, picker.Item{Header: true, Title: gIn, Group: gIn})
		out = append(out, have...)
	}
	if len(in.profiles) > 0 {
		out = append(out, picker.Item{Header: true, Title: gBuilt, Group: gBuilt})
		for _, p := range in.profiles {
			hint := "termocode AI · " + strconv.Itoa(p.steps) + " step"
			if p.steps != 1 {
				hint += "s"
			}
			out = append(out, picker.Item{ID: agentPickProfilePfx + p.name, Title: aiSpark() + " " + p.name, Hint: hint, Group: gBuilt})
		}
	}
	if len(missing) > 0 {
		out = append(out, picker.Item{Header: true, Title: gNot, Group: gNot})
		out = append(out, missing...)
	}
	return out
}

// agentPickInputNow is the input from this machine and session.
func (m *Model) agentPickInputNow() agentPickInput {
	s := m.aiS()
	s.reloadProfiles()
	in := agentPickInput{agents: agents.Registry, installed: agentInstalled, cli: s.cliStates, accounts: loadSettings().AgentAccounts}
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		in.profiles = append(in.profiles, agentPickProfile{name: n, steps: len(p.Steps)})
	}
	if m.lspMgr != nil {
		in.installing = m.lspMgr.installing
	}
	return in
}

// openAgentPicker opens Agent: Start… and checks the CLIs' sign-in in
// the background (the rows update when the answer comes).
func (m *Model) openAgentPicker() tea.Cmd {
	// From the WORKTREES view the agent starts in the selected worktree
	// (AGX-2); anywhere else in the workspace.
	m.agentPickDir = ""
	note := agentPickerNote
	if m.worktreesVisible() && m.focus == FocusExplorer {
		if it, ok := m.wtSelected(); ok && wtUsable(it) == "" && !it.here {
			m.agentPickDir = it.e.Path
			note = "Enter starts it in " + wtShortPath(it.e.Path) + " in a new terminal tab; a profile opens the AI chat."
		}
	}
	m.picker = picker.NewItems(" Start an agent ", agentPickerItems(m.agentPickInputNow()))
	m.picker.SetNote([]string{note})
	m.picker.SetWidth(agentPickerWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentPicker
	return aiCLICheckCmd()
}

// refreshAgentPicker redraws an open picker's rows (the cursor stays).
func (m *Model) refreshAgentPicker() {
	if m.pickerOpen && m.pickerKind == pickerKindAgentPicker {
		m.picker.SetItems(agentPickerItems(m.agentPickInputNow()))
	}
}

// onAgentPicked runs a picker row.
func (m *Model) onAgentPicked(id string) tea.Cmd {
	if name, ok := strings.CutPrefix(id, agentPickProfilePfx); ok {
		_, cmd, err := m.aiToolProfileSwitch(name) // a project profile asks for trust first
		if err != nil {
			return m.aiInfo(err.Error())
		}
		return tea.Batch(cmd, m.openAIPanel(true))
	}
	aid, ok := strings.CutPrefix(id, agentPickAgentPfx)
	if !ok {
		return nil
	}
	if cli, label, as := strings.Cut(aid, "@"); as { // an account row (agent_accounts.go)
		return m.onAgentAccountPicked(agentAccountPfx + cli + "@" + label)
	}
	a, ok := agents.ByID(aid)
	if !ok {
		return nil
	}
	return m.onAgentPickedRow(a) // start, sign in or install (agent_install.go)
}

func init() {
	aiActionPolicies[agentPickerPaletteID] = aiNever(aiNoPicker + " (outside_agent_start)")
}
