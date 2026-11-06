package app

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent account profiles (AGX-13, owner decision D2) ──────────────────
//
// A named account of Claude Code or Codex ("work", "personal") is its own
// config folder under termocode's config folder:
// agents/<cli>/<label>/, mode 0700. A launch as that account sets the
// CLI's documented variable (CLAUDE_CONFIG_DIR, CODEX_HOME) on the line
// with env, so the CLI keeps its own sign-in files there. termocode stores
// only the labels (config.json agent_accounts), never reads the sign-in
// files there (with agent_transcripts = read only the session files, as
// in ~/.claude), and never touches ~/.claude or ~/.codex: there is no in-place
// credential swap (D2; AGX-14 is not built). A CLI without such a
// variable has no accounts.

const (
	pickerKindAgentAccounts       pickerKindEnum  = 3744
	promptKindAgentAccountNew     promptKindEnum  = 3745
	confirmKindAgentAccountSignIn confirmKindEnum = 3746
	agentAccountsPaletteID                        = "agent-account-use"
	agentAccountPfx                               = "account:"
	agentAccountAddPfx                            = "account-add:"
)

// agentStartAs starts agent a as account label (a var for tests).
var agentStartAs = func(m *Model, a agents.Agent, label string) tea.Cmd { return m.startAgentAs(a, label) }

var agentAccountLabelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}$`)

// agentAccountLabelOK checks a label: 1–24 of a-z 0-9 _ -, not first - or _.
func agentAccountLabelOK(label string) error {
	if !agentAccountLabelRe.MatchString(label) {
		return errors.New("use a short label: lower-case letters, digits, - and _ (up to 24)")
	}
	return nil
}

// agentAccountsRoot is the folder of every account folder.
func agentAccountsRoot() (string, error) {
	cfg, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "agents"), nil
}

// agentAccountDir is the config folder of cli's account label.
func agentAccountDir(cli, label string) (string, error) {
	if err := agentAccountLabelOK(label); err != nil {
		return "", err
	}
	if a, ok := agents.ByID(cli); !ok || a.ConfigEnv == "" {
		return "", errors.New(cli + " has no accounts: it has no config-folder variable")
	}
	root, err := agentAccountsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, cli, label), nil
}

// agentAccountEnsureDir makes the folder (0700, also when it was there).
func agentAccountEnsureDir(cli, label string) (string, error) {
	dir, err := agentAccountDir(cli, label)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// agentAccountLine is base run as account label: "env VAR=<dir> base";
// base itself for the default account ("").
func agentAccountLine(a agents.Agent, label, base string) (string, error) {
	if label == "" {
		return base, nil
	}
	if a.ConfigEnv == "" {
		return "", errors.New(a.Title + " has no accounts: it has no config-folder variable")
	}
	dir, err := agentAccountEnsureDir(a.ID, label)
	if err != nil {
		return "", err
	}
	w, ok := agents.ShellWord(dir)
	if !ok {
		return "", errors.New("the account folder has a character the shell line cannot quote")
	}
	return "env " + a.ConfigEnv + "=" + w + " " + base, nil
}

// agentAccountLabels are the saved labels of cli.
func agentAccountLabels(cli string) []string {
	return loadSettings().AgentAccounts[cli]
}

// agentAccountsWithEnv are the registry agents that can have accounts.
func agentAccountsWithEnv() (with, without []agents.Agent) {
	for _, a := range agents.Registry {
		if a.ConfigEnv != "" {
			with = append(with, a)
		} else if a.Palette {
			without = append(without, a)
		}
	}
	return with, without
}

// agentAccountItems are the rows of Agent: Accounts….
func agentAccountItems() []picker.Item {
	with, without := agentAccountsWithEnv()
	var out []picker.Item
	for _, a := range with {
		out = append(out, picker.Item{Header: true, Title: strings.ToUpper(a.Title), Group: a.ID})
		for _, l := range agentAccountLabels(a.ID) {
			hint := a.ConfigEnv + " · Enter starts it"
			if !agentInstalled(a) {
				hint = a.Bin + " not installed"
			}
			out = append(out, picker.Item{ID: agentAccountPfx + a.ID + "@" + l, Title: a.Title + " · " + l, Hint: hint, Group: a.ID})
		}
		out = append(out, picker.Item{ID: agentAccountAddPfx + a.ID, Title: "Add a " + a.Title + " account…", Hint: "its own config folder", Group: a.ID})
	}
	if len(without) > 0 {
		var names []string
		for _, a := range without {
			names = append(names, a.Title)
		}
		out = append(out, picker.Item{Header: true, Title: "NO ACCOUNTS", Group: "none"},
			picker.Item{ID: "none", Title: strings.Join(names, ", "), Hint: "no accounts: no config-folder variable", Group: "none"})
	}
	return out
}

// openAgentAccounts is Agent: Accounts….
func (m *Model) openAgentAccounts() tea.Cmd {
	m.picker = picker.NewItems(" Agent accounts ", agentAccountItems())
	m.picker.SetNote([]string{"Each account has its own config folder; termocode keeps only the label, never its sign-in."})
	m.picker.SetWidth(agentPickerWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentAccounts
	return nil
}

// onAgentAccountPicked starts an account or asks for a new label.
func (m *Model) onAgentAccountPicked(id string) tea.Cmd {
	if cli, ok := strings.CutPrefix(id, agentAccountAddPfx); ok {
		a, known := agents.ByID(cli)
		if !known || a.ConfigEnv == "" {
			return nil
		}
		m.aiS().agentPickPending = cli
		m.prompt = prompt.New(" New "+a.Title+" account ", "A short label for the account (work, personal):", "")
		m.prompt.SetNote("It gets its own config folder under termocode's config; you sign in there next.")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindAgentAccountNew
		return nil
	}
	rest, ok := strings.CutPrefix(id, agentAccountPfx)
	if !ok {
		return nil
	}
	cli, label, _ := strings.Cut(rest, "@")
	a, known := agents.ByID(cli)
	if !known || agentAccountLabelOK(label) != nil {
		return nil
	}
	return agentStartAs(m, a, label)
}

// onAgentAccountLabel saves a new account's label and folder, then asks
// to sign in.
func (m *Model) onAgentAccountLabel(value string) tea.Cmd {
	cli := m.aiS().agentPickPending
	m.aiS().agentPickPending = ""
	label := strings.TrimSpace(value)
	if err := agentAccountLabelOK(label); err != nil {
		return m.pushToastDetail(toast.Warn, "Account not added", err.Error())
	}
	if _, err := agentAccountEnsureDir(cli, label); err != nil {
		return m.pushToastDetail(toast.Errr, "Account not added", err.Error())
	}
	if err := saveAgentAccount(cli, label); err != nil {
		return m.pushToastDetail(toast.Errr, "Account not saved", err.Error())
	}
	return m.onAgentAccountLabelSignIn(cli, label)
}

// onAgentAccountLabelSignIn opens the sign-in card of an account.
func (m *Model) onAgentAccountLabelSignIn(cli, label string) tea.Cmd {
	a, ok := agents.ByID(cli)
	if !ok || a.Login == "" {
		return nil
	}
	line, err := agentAccountLine(a, label, a.Login)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot sign in", err.Error())
	}
	m.aiS().agentAccountPending = line
	body := "Runs in a new terminal tab:\n\n    " + line +
		"\n\nThe CLI keeps this account's sign-in in that folder. termocode never reads the sign-in and never sees your password or token."
	m.confirm = confirm.Risky("Sign in to "+a.Title+" as "+label+"?", body,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "signin", Title: "Sign in", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentAccountSignIn
	return nil
}

// answerAgentAccountCard runs Sign in; Not now and Esc run nothing.
func (m *Model) answerAgentAccountCard(id string) tea.Cmd {
	line := m.aiS().agentAccountPending
	m.aiS().agentAccountPending = ""
	m.confirmOpen = false
	if id != "signin" || line == "" {
		return nil
	}
	return agentPickRunLine(m, line)
}

// saveAgentAccount adds label to cli's accounts in config.json.
func saveAgentAccount(cli, label string) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(cfg map[string]any) error {
		all, _ := cfg["agent_accounts"].(map[string]any)
		if all == nil {
			all = map[string]any{}
		}
		list, _ := all[cli].([]any)
		for _, v := range list {
			if v == label {
				return nil
			}
		}
		all[cli] = append(list, label)
		cfg["agent_accounts"] = all
		return nil
	})
}

// agentAccountsRedact hides the account folders in s (bug reports).
func agentAccountsRedact(s string) string {
	root, err := agentAccountsRoot()
	if err != nil || root == "" {
		return s
	}
	return strings.ReplaceAll(s, root, "[agent account folders]")
}

func init() {
	aiActionPolicies[agentAccountsPaletteID] = aiNever(aiNoAIConfig + "; " + aiNoPicker)
	aiSettingPolicies["agent_accounts"] = aiNever(aiNoAIConfig + "; the labels of the outside agents' account folders")
}
