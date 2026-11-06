package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
)

// accountsTestModel is a model under a fake HOME and config folder with
// Claude Code and Codex "installed"; started agents and run lines are
// recorded, nothing runs.
func accountsTestModel(t *testing.T) (m *Model, home string, started *[]string, lines *[]string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	mm := aiTestModel(t) // sets XDG_CONFIG_HOME to a temp folder
	m = &mm
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	fakeAgentLookPath(t, "claude", "codex", "gemini")
	var s, l []string
	oldStart := agentStartAs
	agentStartAs = func(_ *Model, a agents.Agent, label string) tea.Cmd { s = append(s, a.ID+"@"+label); return nil }
	t.Cleanup(func() { agentStartAs = oldStart })
	oldRun := agentPickRunLine
	agentPickRunLine = func(_ *Model, line string) tea.Cmd { l = append(l, line); return nil }
	t.Cleanup(func() { agentPickRunLine = oldRun })
	return m, home, &s, &l
}

// Spec (AGX-13): only the CLIs with a documented config-folder variable
// get accounts: Claude Code (CLAUDE_CONFIG_DIR) and Codex (CODEX_HOME).
func TestAgentConfigEnv(t *testing.T) {
	for _, a := range agents.Registry {
		want := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}[a.ID]
		if a.ConfigEnv != want {
			t.Errorf("%s ConfigEnv %q, want %q", a.ID, a.ConfigEnv, want)
		}
	}
}

// Spec (AGX-13): a label is a short lower-case name; anything that
// could leave the folder or break the shell line is refused.
func TestAgentAccountLabel(t *testing.T) {
	for _, ok := range []string{"work", "personal", "client-2", "a_b"} {
		if err := agentAccountLabelOK(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "..", "../x", "a/b", "Work Space", "a'b", "$(id)", "-x", strings.Repeat("a", 25)} {
		if agentAccountLabelOK(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Spec (AGX-13): an account's folder is under termocode's config
// folder (agents/<cli>/<label>, mode 0700); the launch line sets the
// CLI's variable to it with env (works in every shell); the default
// account has no env, so today's launch is unchanged. Nothing under
// ~/.claude or ~/.codex is made.
func TestAgentAccountDirAndLaunchLine(t *testing.T) {
	_, home, _, _ := accountsTestModel(t)
	claude, _ := agents.ByID("claude")
	codex, _ := agents.ByID("codex")
	dir, err := agentAccountEnsureDir("claude", "work")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := configDir()
	if dir != filepath.Join(cfg, "agents", "claude", "work") {
		t.Errorf("dir %s", dir)
	}
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v %v", st, err)
	}
	if line, err := agentAccountLine(claude, "work", "claude --mcp-config /x"); err != nil || line != "env CLAUDE_CONFIG_DIR="+mustWord(t, dir)+" claude --mcp-config /x" {
		t.Errorf("line %q %v", line, err)
	}
	if line, err := agentAccountLine(codex, "", "codex"); err != nil || line != "codex" {
		t.Errorf("default line %q %v", line, err)
	}
	gem, _ := agents.ByID("gemini")
	if _, err := agentAccountLine(gem, "work", "gemini"); err == nil {
		t.Error("an account for a CLI without a config variable")
	}
	for _, p := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(home, p)); err == nil {
			t.Errorf("%s was made", p)
		}
	}
}

func mustWord(t *testing.T, s string) string {
	t.Helper()
	w, ok := agents.ShellWord(s)
	if !ok {
		t.Fatalf("cannot quote %q", s)
	}
	return w
}

// Spec (AGX-13): Agent: Accounts… lists the accounts of Claude Code and
// Codex (Enter starts the agent as that account) and an Add row per CLI;
// a CLI without a config variable has a row that says so. The AI cannot
// open it, nor change agent_accounts.
func TestAgentAccountsPicker(t *testing.T) {
	m, _, started, _ := accountsTestModel(t)
	writeSettingsJSON(t, `{"agent_accounts": {"claude": ["work"]}}`)
	if p, ok := aiActionPolicyFor(agentAccountsPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	if p, ok := aiSettingPolicies["agent_accounts"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentAccountsPaletteID && it.Title == "Agent: Accounts…" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	m.dispatchTasksPalette(agentAccountsPaletteID)
	if !m.pickerOpen || m.pickerKind != pickerKindAgentAccounts {
		t.Fatalf("picker %v %v", m.pickerOpen, m.pickerKind)
	}
	view := ansi.Strip(m.picker.View())
	for _, w := range []string{"Claude Code · work", "Add a Claude Code account", "Add a Codex account", "Gemini CLI", "no accounts"} {
		if !strings.Contains(view, w) {
			t.Errorf("no %q in\n%s", w, view)
		}
	}
	m.pickerOpen = false
	m.onAgentAccountPicked(agentAccountPfx + "claude@work")
	if len(*started) != 1 || (*started)[0] != "claude@work" {
		t.Errorf("started %q", *started)
	}
}

// Spec (AGX-13): Add asks for a label; a bad one is refused and nothing
// is saved; a good one is saved (labels only) with its 0700 folder, then
// the sign-in card shows the CLI's own login line with the account's
// variable, Not now first. Not now runs nothing; Sign in runs that line.
func TestAgentAccountAdd(t *testing.T) {
	m, _, _, lines := accountsTestModel(t)
	m.onAgentAccountPicked(agentAccountAddPfx + "codex")
	if !m.promptOpen || m.promptKind != promptKindAgentAccountNew {
		t.Fatalf("prompt %v %v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.onAgentAccountLabel("Bad Label")
	if !strings.Contains(toastText(*m), "label") || len(loadSettings().AgentAccounts["codex"]) != 0 {
		t.Errorf("bad label: toast %q saved %v", toastText(*m), loadSettings().AgentAccounts)
	}
	m.onAgentAccountPicked(agentAccountAddPfx + "codex")
	m.promptOpen = false
	m.onAgentAccountLabel("personal")
	if got := loadSettings().AgentAccounts["codex"]; len(got) != 1 || got[0] != "personal" {
		t.Errorf("saved %v", got)
	}
	path, _ := settingsConfigPath()
	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	if strings.Contains(string(raw), "token") || strings.Contains(string(raw), "agents/codex") {
		t.Errorf("config holds more than labels: %s", raw)
	}
	dir, _ := agentAccountDir("codex", "personal")
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("dir %v %v", st, err)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAgentAccountSignIn || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %v %v %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	if !strings.Contains(box, "CODEX_HOME=") || !strings.Contains(box, "codex login") {
		t.Errorf("card:\n%s", box)
	}
	m.answerAgentAccountCard("cancel")
	if len(*lines) != 0 {
		t.Errorf("Not now ran %q", *lines)
	}
	m.onAgentAccountLabelSignIn("codex", "personal")
	m.answerAgentAccountCard("signin")
	if len(*lines) != 1 || !strings.HasPrefix((*lines)[0], "env CODEX_HOME=") || !strings.HasSuffix((*lines)[0], " codex login") {
		t.Errorf("ran %q", *lines)
	}
}

// Spec (AGX-13): Agent: Start… has a row per saved account under its
// installed agent.
func TestAgentPickerAccountRows(t *testing.T) {
	claude, _ := agents.ByID("claude")
	items := agentPickerItems(agentPickInput{agents: []agents.Agent{claude}, installed: func(agents.Agent) bool { return true },
		accounts: map[string][]string{"claude": {"work"}}})
	found := false
	for _, it := range items {
		if it.ID == agentPickAgentPfx+"claude@work" && strings.Contains(it.Title, "as work") {
			found = true
		}
	}
	if !found {
		t.Errorf("no account row in %+v", items)
	}
}

// Spec (AGX-13): an account tab's plan window is its own: "Claude work".
func TestAgentAccountUsageKey(t *testing.T) {
	agentsUnicode(t)
	m, screen, _ := limitTestModel(t)
	m.terminalTabs[0].Account = "work"
	*screen = limitScreen(t, "limit/codex-usage.txt")
	m.onAgentOutput(7)
	if st := m.statusState(); st.Plan != "✦ Codex work limit ↻12:05" {
		t.Errorf("pill %q", st.Plan)
	}
}

// Spec (AGX-13): the account folders (they hold the CLIs' own sign-in
// files) never show in a bug report.
func TestAgentAccountNotInBugReport(t *testing.T) {
	_, _, _, _ = accountsTestModel(t)
	dir, _ := agentAccountDir("claude", "work")
	body := buildBugReport(nil, "open "+dir+"/.credentials.json: denied", "")
	if strings.Contains(body, dir) || strings.Contains(body, "agents/claude/work") {
		t.Errorf("the folder shows:\n%s", body)
	}
}
