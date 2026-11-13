package app

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/agentswap"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Agent: Switch Account… (AGX-14, owner D2): the in-place switch of the
// signed-in account of Claude Code or Codex. It writes the CLI's own
// credential file in your home, so it is off until agent_account_swap is
// true in config.json. The account files live in the secret store; a
// switch shows a card (Cancel first, the file and its size, the contents
// hidden), keeps the old file as <file>.termocode-backup and can be
// undone. It is refused while a tab of that CLI runs. (AGX-13 accounts,
// which never touch ~/.claude or ~/.codex, stay the safer way.)

const agentSwapPaletteID = "agent-swap-account"

func init() {
	registerGitx(gitxFeature{
		palette:  []picker.Item{{ID: agentSwapPaletteID, Title: "Agent: Switch Account…", Hint: "agent_account_swap"}},
		policies: map[string]aiActionPolicy{agentSwapPaletteID: aiNever(aiNoAIConfig + "; it replaces the credential file of an outside agent")},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id == agentSwapPaletteID {
				return m.openAgentSwap(), true
			}
			return nil, false
		},
	})
	aiSettingPolicies["agent_account_swap"] = aiNever(aiNoAIConfig + "; it lets termocode replace the credential file of an outside agent")
}

// agentAccountSwapOn is the agent_account_swap setting (off by default).
func agentAccountSwapOn() bool { return loadSettings().AgentAccountSwap }

// swapper builds the Swapper of this machine.
func (m *Model) swapper() (agentswap.Swapper, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return agentswap.Swapper{}, err
	}
	var v agentswap.Vault
	if m.aiS().swapVault != nil {
		v = m.aiS().swapVault
	} else {
		st, err := configureSecretStore()
		if err != nil {
			return agentswap.Swapper{}, err
		}
		v = st
	}
	return agentswap.Swapper{Home: home, Vault: v, GOOS: runtime.GOOS, Running: m.agentCLIRunning}, nil
}

// agentCLIRunning: a terminal tab runs that agent.
func (m *Model) agentCLIRunning(cli string) bool {
	for _, set := range [][]terminalTab{m.terminalTabs, m.hiddenTerminalTabs} {
		for _, t := range set {
			if t.Agent == cli {
				return true
			}
		}
	}
	return false
}

// openAgentSwap lists the CLIs.
func (m *Model) openAgentSwap() tea.Cmd {
	if !agentAccountSwapOn() {
		return m.pushToastDetail(toast.Info, "Account switching is off",
			"It replaces the CLI's own sign-in file in your home. Set agent_account_swap to true in config.json to allow it. Agent: Accounts… never touches those files.")
	}
	sw, err := m.swapper()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot switch accounts", err.Error())
	}
	var items []picker.Item
	for _, a := range agents.Registry {
		if a.ConfigEnv == "" {
			continue
		}
		ok, why := sw.Supported(a.ID)
		hint := "sign-in file in your home"
		if !ok {
			hint = why
		}
		items = append(items, picker.Item{ID: "cli-" + a.ID, Title: a.Title, Hint: hint})
	}
	m.gitxPick(" Switch account of which agent? ", items, func(m *Model, id string) tea.Cmd {
		return m.openAgentSwapCLI(strings.TrimPrefix(id, "cli-"))
	})
	return nil
}

// agentSwapLabels are the saved accounts of cli, from the vault's index of names.
func (m *Model) agentSwapLabels(cli string) []string {
	var names []string
	if sv := m.aiS().swapVault; sv != nil {
		if l, ok := sv.(interface{ Names() []string }); ok {
			names = l.Names()
		}
	} else if st, err := configureSecretStore(); err == nil {
		names = st.Names()
	}
	pfx := agentswap.SecretName(cli, "")
	var out []string
	for _, n := range names {
		if l, ok := strings.CutPrefix(n, pfx); ok && agentswap.ValidLabel(l) {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// openAgentSwapCLI lists capture, the saved accounts and undo.
func (m *Model) openAgentSwapCLI(cli string) tea.Cmd {
	sw, err := m.swapper()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot switch accounts", err.Error())
	}
	if ok, why := sw.Supported(cli); !ok {
		return m.pushToastDetail(toast.Info, "Not available", why)
	}
	a, _ := agents.ByID(cli)
	items := []picker.Item{{ID: "capture", Title: "Save the signed-in account as…", Hint: "keeps a copy in the secret store"}}
	for _, l := range m.agentSwapLabels(cli) {
		items = append(items, picker.Item{ID: "use-" + l, Title: "Use " + l, Hint: "replaces the sign-in file (asks first)"})
	}
	items = append(items, picker.Item{ID: "undo", Title: "Undo the last switch", Hint: "puts the old file back"})
	m.gitxPick(" "+a.Title+" accounts ", items, func(m *Model, id string) tea.Cmd {
		switch {
		case id == "capture":
			m.gitxPrompt(" Name this "+a.Title+" account ", "A short name (work, personal):", "", func(m *Model, v string) tea.Cmd {
				v = strings.TrimSpace(v)
				if err := sw.Capture(cli, v); err != nil {
					return m.pushToastDetail(toast.Warn, "Account not saved", err.Error())
				}
				return m.pushToastDetail(toast.Info, "Saved "+v, "The sign-in file is kept in the secret store; its contents are never shown.")
			})
		case id == "undo":
			return m.agentSwapUndoCard(sw, cli)
		case strings.HasPrefix(id, "use-"):
			return m.agentSwapUseCard(sw, cli, strings.TrimPrefix(id, "use-"))
		}
		return nil
	})
	return nil
}

func (m *Model) agentSwapUseCard(sw agentswap.Swapper, cli, label string) tea.Cmd {
	pv, err := sw.Preview(cli, label)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot use "+label, err.Error())
	}
	if sw.Running != nil && sw.Running(cli) {
		return m.pushToastDetail(toast.Warn, "Cannot use "+label, agentswap.ErrRunning.Error())
	}
	body := fmt.Sprintf("File: %s\nSize: %d bytes (contents hidden)\n\nThe file now there is kept as %s%s. Every new %s run uses %s. Undo: Agent: Switch Account… → Undo.",
		pv.Path, pv.Size, pv.Path, agentswap.BackupSuffix, cli, label)
	m.gitxCard("Use account "+label+" for "+cli+"?", body, func(m *Model, id string) tea.Cmd {
		if id != "apply" {
			return nil
		}
		if err := sw.Apply(cli, label); err != nil {
			return m.pushToastDetail(toast.Errr, "Account not switched", err.Error())
		}
		return m.pushToastDetail(toast.Info, "Using "+label, "Undo: Agent: Switch Account… → Undo the last switch.")
	}, confirm.Button{ID: "apply", Title: "Switch", Style: confirm.StyleDestructive})
	return nil
}

func (m *Model) agentSwapUndoCard(sw agentswap.Swapper, cli string) tea.Cmd {
	m.gitxCard("Put the old sign-in file back?", "The file kept as "+agentswap.BackupSuffix+" replaces the current one (contents hidden).", func(m *Model, id string) tea.Cmd {
		if id != "undo" {
			return nil
		}
		if err := sw.Undo(cli); err != nil {
			return m.pushToastDetail(toast.Warn, "Nothing undone", err.Error())
		}
		return m.pushToastDetail(toast.Info, "Switched back", "")
	}, confirm.Button{ID: "undo", Title: "Undo", Style: confirm.StyleDestructive})
	return nil
}
