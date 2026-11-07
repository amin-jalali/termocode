package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
)

// profilesTestModel is accountsTestModel with the profile starts
// recorded ("cli#profile"); nothing runs.
func profilesTestModel(t *testing.T) (m *Model, started *[]string) {
	t.Helper()
	m, _, _, _ = accountsTestModel(t)
	var s []string
	old := agentStartProfile
	agentStartProfile = func(_ *Model, a agents.Agent, p agents.LaunchProfile) tea.Cmd {
		s = append(s, a.ID+"#"+p.Name)
		return nil
	}
	t.Cleanup(func() { agentStartProfile = old })
	return m, &s
}

// Spec (CMP-21): Agent: Profiles… is a palette row the AI can never run
// (it is the user's launch config); agent_profiles is a file-only
// setting the AI can never change. The picker lists the saved profiles
// with their summary and an Add row per agent with flags.
func TestAgentProfilesPicker(t *testing.T) {
	m, started := profilesTestModel(t)
	writeSettingsJSON(t, `{"agent_profiles": {"claude": [{"name": "review", "model": "opus", "mode": "plan"}]}}`)
	if p, ok := aiActionPolicyFor(agentProfilesPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	if p, ok := aiSettingPolicies["agent_profiles"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentProfilesPaletteID && it.Title == "Agent: Profiles…" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	m.dispatchTasksPalette(agentProfilesPaletteID)
	if !m.pickerOpen || m.pickerKind != pickerKindAgentProfiles {
		t.Fatalf("picker %v %v", m.pickerOpen, m.pickerKind)
	}
	view := ansi.Strip(m.picker.View())
	for _, w := range []string{"Claude Code · review", "opus · Plan (read only)", "Add a Claude Code profile", "Add a Codex profile"} {
		if !strings.Contains(view, w) {
			t.Errorf("no %q in\n%s", w, view)
		}
	}
	// A profile row opens its card: Cancel first; Start starts it.
	m.pickerOpen = false
	m.onAgentProfilePicked(agentProfilePfx + "claude#review")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentProfileUse || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %v %v %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	if !strings.Contains(box, "claude --model opus --permission-mode plan") {
		t.Errorf("card:\n%s", box)
	}
	m.answerAgentProfileCard("cancel")
	if len(*started) != 0 {
		t.Errorf("Cancel started %q", *started)
	}
	m.onAgentProfilePicked(agentProfilePfx + "claude#review")
	m.answerAgentProfileCard("start")
	if len(*started) != 1 || (*started)[0] != "claude#review" {
		t.Errorf("started %q", *started)
	}
	// Delete removes it from config.json.
	m.onAgentProfilePicked(agentProfilePfx + "claude#review")
	m.answerAgentProfileCard("delete")
	if got := loadSettings().AgentProfiles["claude"]; len(got) != 0 {
		t.Errorf("not deleted: %+v", got)
	}
}

// Spec (CMP-21): Add asks for a name, then the mode (a picker of the
// agent's modes), the model and the env; the Save card shows the whole
// launch line, Cancel first. Cancel saves nothing; Save writes the
// profile to config.json. A bad value stops the flow with a toast.
func TestAgentProfileAdd(t *testing.T) {
	m, _ := profilesTestModel(t)
	add := func() {
		m.onAgentProfilePicked(agentProfileAddPfx + "codex")
		if !m.promptOpen || m.promptKind != promptKindAgentProfileName {
			t.Fatalf("name prompt %v %v", m.promptOpen, m.promptKind)
		}
		m.promptOpen = false
		m.onAgentProfileName("safe")
		if !m.pickerOpen || m.pickerKind != pickerKindAgentProfileMode {
			t.Fatalf("mode picker %v %v", m.pickerOpen, m.pickerKind)
		}
		view := ansi.Strip(m.picker.View())
		if !strings.Contains(view, "Plan (read only)") || !strings.Contains(view, "CLI default") {
			t.Errorf("modes:\n%s", view)
		}
		m.pickerOpen = false
		m.onAgentProfileModePicked("plan")
		if !m.promptOpen || m.promptKind != promptKindAgentProfileModel {
			t.Fatalf("model prompt %v %v", m.promptOpen, m.promptKind)
		}
		m.promptOpen = false
		m.onAgentProfileModel("gpt-5")
		if !m.promptOpen || m.promptKind != promptKindAgentProfileEnv {
			t.Fatalf("env prompt %v %v", m.promptOpen, m.promptKind)
		}
		m.promptOpen = false
		m.onAgentProfileEnv("RUST_LOG=info")
		if !m.confirmOpen || m.confirmKind != confirmKindAgentProfileSave || m.confirm.SafeID() != "cancel" {
			t.Fatalf("save card %v %v %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
		}
	}
	add()
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	if !strings.Contains(box, "env RUST_LOG=info codex --model gpt-5 --sandbox read-only --ask-for-approval on-request") {
		t.Errorf("card:\n%s", box)
	}
	m.answerAgentProfileSave("cancel")
	if got := loadSettings().AgentProfiles["codex"]; len(got) != 0 {
		t.Errorf("Cancel saved %+v", got)
	}
	add()
	m.answerAgentProfileSave("save")
	got := loadSettings().AgentProfiles["codex"]
	if len(got) != 1 || got[0].Name != "safe" || got[0].Model != "gpt-5" || got[0].Mode != "plan" || got[0].Env["RUST_LOG"] != "info" {
		t.Errorf("saved %+v", got)
	}

	// A secret-looking variable is refused with a toast; nothing saved.
	m.onAgentProfilePicked(agentProfileAddPfx + "claude")
	m.promptOpen = false
	m.onAgentProfileName("k")
	m.pickerOpen = false
	m.onAgentProfileModePicked("")
	m.promptOpen = false
	m.onAgentProfileModel("")
	m.promptOpen = false
	m.onAgentProfileEnv("ANTHROPIC_API_KEY=sk-123")
	if m.confirmOpen || !strings.Contains(toastText(*m), "secret") {
		t.Errorf("secret env: card %v toast %q", m.confirmOpen, toastText(*m))
	}
	path, _ := settingsConfigPath()
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "sk-123") {
		t.Errorf("the secret was written: %s", raw)
	}
}

// Spec (CMP-21): Agent: Start… has a row per saved profile under its
// installed agent; a profile with a safe mode starts at once, one that
// skips the permission checks shows its card first (Cancel first).
func TestAgentPickerProfileRows(t *testing.T) {
	claude, _ := agents.ByID("claude")
	items := agentPickerItems(agentPickInput{agents: []agents.Agent{claude}, installed: func(agents.Agent) bool { return true },
		profiles: nil, launchProfiles: map[string][]agents.LaunchProfile{"claude": {{Name: "review", Mode: "plan"}}}})
	found := false
	for _, it := range items {
		if it.ID == agentPickAgentPfx+"claude#review" && strings.Contains(it.Title, "profile review") {
			found = true
		}
	}
	if !found {
		t.Errorf("no profile row in %+v", items)
	}

	m, started := profilesTestModel(t)
	writeSettingsJSON(t, `{"agent_profiles": {"claude": [{"name": "review", "mode": "plan"}, {"name": "wild", "mode": "bypass"}]}}`)
	m.onAgentPicked(agentPickAgentPfx + "claude#review")
	if len(*started) != 1 || (*started)[0] != "claude#review" || m.confirmOpen {
		t.Errorf("safe profile: started %q card %v", *started, m.confirmOpen)
	}
	m.onAgentPicked(agentPickAgentPfx + "claude#wild")
	if len(*started) != 1 || !m.confirmOpen || m.confirmKind != confirmKindAgentProfileUse || m.confirm.SafeID() != "cancel" {
		t.Errorf("risky profile: started %q card %v %v", *started, m.confirmOpen, m.confirmKind)
	}
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	if !strings.Contains(box, "--permission-mode bypassPermissions") || !strings.Contains(box, "no permission checks") {
		t.Errorf("risky card:\n%s", box)
	}
}

// Spec (CMP-21): the launch line of a profile tab carries the profile's
// flags after the agent (and its MCP flags) and its env before it, and
// the cd into a worktree stays first.
func TestAgentProfileLaunchLine(t *testing.T) {
	m, _ := profilesTestModel(t)
	claude, _ := agents.ByID("claude")
	p := agents.LaunchProfile{Name: "r", Model: "opus", Mode: "plan", Env: map[string]string{"X_MODE": "1"}}
	line, _, err := m.agentProfileLaunchLine(claude, "/w/feat", &p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "cd /w/feat && env X_MODE=1 claude") || !strings.HasSuffix(line, " --model opus --permission-mode plan") {
		t.Errorf("line %q", line)
	}
}
