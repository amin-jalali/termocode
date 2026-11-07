package app

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Outside-agent launch profiles (CMP-21) ──────────────────────────────
//
// A profile is a named way to start an outside agent: its model, a
// permission mode (plan mode where the CLI has one) and extra env
// variables (agents/profiles.go has the flags). Agent: Profiles… lists,
// adds, starts and deletes them; Agent: Start… has a row per profile
// under its installed agent. Every launch line is shown before it is
// saved; a mode that skips the permission checks shows a Cancel-first
// card at every start. Profiles live in config.json agent_profiles (file
// only); a secret-looking variable is refused, so no key is stored there.
// The AI can neither open these pickers nor change the setting.

const (
	pickerKindAgentProfiles     pickerKindEnum  = 3810
	pickerKindAgentProfileMode  pickerKindEnum  = 3811
	promptKindAgentProfileName  promptKindEnum  = 3812
	promptKindAgentProfileModel promptKindEnum  = 3813
	promptKindAgentProfileEnv   promptKindEnum  = 3814
	confirmKindAgentProfileSave confirmKindEnum = 3815
	confirmKindAgentProfileUse  confirmKindEnum = 3816
	agentProfilesPaletteID                      = "agent-profiles"
	agentProfilePfx                             = "profile-use:"
	agentProfileAddPfx                          = "profile-add:"
	agentProfileDefaultMode                     = "default"
	agentProfileRiskyNote                       = "This mode has no permission checks: the agent edits files and runs commands without asking."
)

// agentProfileRef is one agent's profile (a draft or the open card's).
type agentProfileRef struct {
	cli string
	p   agents.LaunchProfile
}

// agentStartProfile starts a with profile p (a var for tests).
var agentStartProfile = func(m *Model, a agents.Agent, p agents.LaunchProfile) tea.Cmd {
	return m.startAgentFull(a, m.agentPickDir, "", "", &p)
}

// agentProfileName is p's name ("" for nil).
func agentProfileName(p *agents.LaunchProfile) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// agentLaunchProfiles are the saved profiles of cli.
func agentLaunchProfiles(cli string) []agents.LaunchProfile {
	return loadSettings().AgentProfiles[cli]
}

// agentFindProfile is cli's profile name.
func agentFindProfile(cli, name string) (agents.LaunchProfile, bool) {
	if name == "" {
		return agents.LaunchProfile{}, false
	}
	for _, p := range agentLaunchProfiles(cli) {
		if p.Name == name {
			return p, true
		}
	}
	return agents.LaunchProfile{}, false
}

// agentProfileRisky says whether p's mode skips the permission checks.
func agentProfileRisky(cli string, p agents.LaunchProfile) bool {
	md, _ := agents.ProfileModeOf(cli, p.Mode)
	return md.Risky
}

// agentProfileItems are the rows of Agent: Profiles….
func agentProfileItems() []picker.Item {
	var out []picker.Item
	for _, a := range agents.Registry {
		if !agents.HasProfileFlags(a.ID) {
			continue
		}
		out = append(out, picker.Item{Header: true, Title: strings.ToUpper(a.Title), Group: a.ID})
		for _, p := range agentLaunchProfiles(a.ID) {
			hint := agents.ProfileSummary(a.ID, p)
			if !agentInstalled(a) {
				hint += " · not installed"
			}
			out = append(out, picker.Item{ID: agentProfilePfx + a.ID + "#" + p.Name, Title: a.Title + " · " + p.Name, Hint: hint, Group: a.ID})
		}
		out = append(out, picker.Item{ID: agentProfileAddPfx + a.ID, Title: "Add a " + a.Title + " profile…", Hint: "model, mode, env", Group: a.ID})
	}
	return out
}

// openAgentProfiles is Agent: Profiles….
func (m *Model) openAgentProfiles() tea.Cmd {
	m.agentPickDir = "" // a profile from here starts in the workspace
	m.picker = picker.NewItems(" Agent profiles ", agentProfileItems())
	m.picker.SetNote([]string{"A profile starts the agent with its model, mode and env; Enter shows the line first."})
	m.picker.SetWidth(agentPickerWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentProfiles
	return nil
}

// onAgentProfilePicked opens a profile's card or starts Add.
func (m *Model) onAgentProfilePicked(id string) tea.Cmd {
	if cli, ok := strings.CutPrefix(id, agentProfileAddPfx); ok {
		a, known := agents.ByID(cli)
		if !known || !agents.HasProfileFlags(cli) {
			return nil
		}
		m.aiS().agentProfileDraft = &agentProfileRef{cli: cli}
		m.openAgentProfilePrompt(promptKindAgentProfileName, " New "+a.Title+" profile ",
			"A short name for the profile (review, fast):", "Next you pick the mode, the model and the env; the line is shown before it is saved.")
		return nil
	}
	rest, ok := strings.CutPrefix(id, agentProfilePfx)
	if !ok {
		return nil
	}
	cli, name, _ := strings.Cut(rest, "#")
	a, known := agents.ByID(cli)
	p, found := agentFindProfile(cli, name)
	if !known || !found {
		return nil
	}
	return m.openAgentProfileCard(a, p, true)
}

func (m *Model) openAgentProfilePrompt(kind promptKindEnum, title, label, note string) {
	m.prompt = prompt.New(title, label, "")
	m.prompt.SetNote(note)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

// openAgentProfileCard shows p's launch line: Cancel first, then Start
// (and Delete from Agent: Profiles…).
func (m *Model) openAgentProfileCard(a agents.Agent, p agents.LaunchProfile, withDelete bool) tea.Cmd {
	line, err := agents.ProfileLine(a, p, a.Bin)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Profile "+p.Name+" cannot start", err.Error())
	}
	m.aiS().agentProfileOpen = &agentProfileRef{cli: a.ID, p: p}
	body := "Runs in a new terminal tab (with termocode's MCP flags where the agent has them):\n\n    " + line
	start := confirm.Button{ID: "start", Title: "Start", Style: confirm.StylePrimary}
	if agentProfileRisky(a.ID, p) {
		body += "\n\n" + agentProfileRiskyNote
		start.Style = confirm.StyleDestructive
	}
	buttons := []confirm.Button{start}
	if withDelete {
		buttons = append(buttons, confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	}
	m.confirm = confirm.Risky("Start "+a.Title+" · "+p.Name+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"}, buttons...).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentProfileUse
	return nil
}

// answerAgentProfileCard runs Start or Delete; Cancel and Esc do nothing.
func (m *Model) answerAgentProfileCard(id string) tea.Cmd {
	ref := m.aiS().agentProfileOpen
	m.aiS().agentProfileOpen = nil
	m.confirmOpen = false
	if ref == nil {
		return nil
	}
	a, ok := agents.ByID(ref.cli)
	if !ok {
		return nil
	}
	switch id {
	case "start":
		return agentStartProfile(m, a, ref.p)
	case "delete":
		if err := deleteAgentProfile(ref.cli, ref.p.Name); err != nil {
			return m.pushToastDetail(toast.Errr, "Profile not deleted", err.Error())
		}
		return m.pushToastDetail(toast.Info, "Profile "+ref.p.Name+" deleted", a.Title+" starts as before.")
	}
	return nil
}

// onAgentProfileName checks the name, then asks for the mode (or the
// next step when the agent has no modes).
func (m *Model) onAgentProfileName(value string) tea.Cmd {
	d := m.aiS().agentProfileDraft
	if d == nil {
		return nil
	}
	name := strings.TrimSpace(value)
	if err := agents.ProfileNameOK(name); err != nil {
		m.aiS().agentProfileDraft = nil
		return m.pushToastDetail(toast.Warn, "Profile not added", err.Error())
	}
	if _, dup := agentFindProfile(d.cli, name); dup {
		m.aiS().agentProfileDraft = nil
		return m.pushToastDetail(toast.Warn, "Profile not added", "a profile named "+name+" is there already; delete it first")
	}
	d.p.Name = name
	modes := agents.ProfileModes(d.cli)
	if len(modes) == 0 {
		return m.onAgentProfileModePicked("")
	}
	items := []picker.Item{{ID: agentProfileDefaultMode, Title: "CLI default", Hint: "the agent asks as it always does"}}
	for _, md := range modes {
		hint := strings.Join(md.Args, " ")
		if md.Risky {
			hint = "skips every permission check · " + hint
		}
		items = append(items, picker.Item{ID: md.ID, Title: md.Title, Hint: hint})
	}
	m.picker = picker.NewItems(" Mode of "+name+" ", items)
	m.picker.SetNote([]string{"How much the agent may do without asking."})
	m.picker.SetWidth(agentPickerWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentProfileMode
	return nil
}

// onAgentProfileModePicked keeps the mode, then asks for the model (or
// the env when the agent has no model flag).
func (m *Model) onAgentProfileModePicked(id string) tea.Cmd {
	d := m.aiS().agentProfileDraft
	if d == nil {
		return nil
	}
	if id == agentProfileDefaultMode {
		id = ""
	}
	if _, ok := agents.ProfileModeOf(d.cli, id); !ok {
		return nil
	}
	d.p.Mode = id
	if agents.ModelFlag(d.cli) == "" {
		return m.onAgentProfileModel("")
	}
	m.openAgentProfilePrompt(promptKindAgentProfileModel, " Model of "+d.p.Name+" ",
		"The model name (blank = the CLI's default):", "Passed as "+agents.ModelFlag(d.cli)+"; the CLI checks the name.")
	return nil
}

// onAgentProfileModel keeps the model, then asks for the env.
func (m *Model) onAgentProfileModel(value string) tea.Cmd {
	d := m.aiS().agentProfileDraft
	if d == nil {
		return nil
	}
	d.p.Model = strings.TrimSpace(value)
	a, _ := agents.ByID(d.cli)
	if err := agents.CheckProfile(a, d.p); err != nil {
		m.aiS().agentProfileDraft = nil
		return m.pushToastDetail(toast.Warn, "Profile not added", err.Error())
	}
	m.openAgentProfilePrompt(promptKindAgentProfileEnv, " Env of "+d.p.Name+" ",
		"Extra variables as NAME=value, space between (blank = none):", "No keys or tokens here: keep them in AI: Secret Store…; config.json is plain text.")
	return nil
}

// onAgentProfileEnv keeps the env, then shows the Save card.
func (m *Model) onAgentProfileEnv(value string) tea.Cmd {
	d := m.aiS().agentProfileDraft
	if d == nil {
		return nil
	}
	env, err := agents.ParseEnv(value)
	a, _ := agents.ByID(d.cli)
	if err == nil {
		if len(env) > 0 {
			d.p.Env = env
		}
		err = agents.CheckProfile(a, d.p)
	}
	var line string
	if err == nil {
		line, err = agents.ProfileLine(a, d.p, a.Bin)
	}
	if err != nil {
		m.aiS().agentProfileDraft = nil
		return m.pushToastDetail(toast.Warn, "Profile not added", err.Error())
	}
	body := "Agent: Start… will run it in a new terminal tab as:\n\n    " + line
	if agentProfileRisky(d.cli, d.p) {
		body += "\n\n" + agentProfileRiskyNote + " termocode asks again at every start."
	}
	m.confirm = confirm.Risky("Save the "+a.Title+" profile "+d.p.Name+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "save", Title: "Save", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentProfileSave
	return nil
}

// answerAgentProfileSave saves the draft; Cancel and Esc save nothing.
func (m *Model) answerAgentProfileSave(id string) tea.Cmd {
	d := m.aiS().agentProfileDraft
	m.aiS().agentProfileDraft = nil
	m.confirmOpen = false
	if id != "save" || d == nil {
		return nil
	}
	a, _ := agents.ByID(d.cli)
	if err := saveAgentProfile(d.cli, d.p); err != nil {
		return m.pushToastDetail(toast.Errr, "Profile not saved", err.Error())
	}
	return m.pushToastDetail(toast.Info, "Profile "+d.p.Name+" saved", "Agent: Start… lists it under "+a.Title+".")
}

// agentProfilesUpdate changes cli's profile list in config.json.
func agentProfilesUpdate(cli string, change func([]any) []any) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(cfg map[string]any) error {
		all, _ := cfg["agent_profiles"].(map[string]any)
		if all == nil {
			all = map[string]any{}
		}
		list, _ := all[cli].([]any)
		list = change(list)
		if len(list) == 0 {
			delete(all, cli)
		} else {
			all[cli] = list
		}
		if len(all) == 0 {
			delete(cfg, "agent_profiles")
		} else {
			cfg["agent_profiles"] = all
		}
		return nil
	})
}

// saveAgentProfile adds p to cli's profiles (a profile with its name is
// replaced).
func saveAgentProfile(cli string, p agents.LaunchProfile) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return err
	}
	return agentProfilesUpdate(cli, func(list []any) []any {
		return append(agentProfilesWithout(list, p.Name), entry)
	})
}

// deleteAgentProfile removes cli's profile name.
func deleteAgentProfile(cli, name string) error {
	return agentProfilesUpdate(cli, func(list []any) []any { return agentProfilesWithout(list, name) })
}

func agentProfilesWithout(list []any, name string) []any {
	out := make([]any, 0, len(list))
	for _, v := range list {
		if e, ok := v.(map[string]any); ok && e["name"] == name {
			continue
		}
		out = append(out, v)
	}
	return out
}

// onAgentPickedProfile starts profile name of a from Agent: Start…: at
// once for a safe mode, after the card for one without permission checks.
func (m *Model) onAgentPickedProfile(a agents.Agent, name string) tea.Cmd {
	p, ok := agentFindProfile(a.ID, name)
	if !ok {
		return m.pushToastDetail(toast.Warn, "Profile "+name+" is gone", "Agent: Profiles… lists the saved ones.")
	}
	if agentProfileRisky(a.ID, p) {
		return m.openAgentProfileCard(a, p, false)
	}
	return agentStartProfile(m, a, p)
}

func init() {
	aiActionPolicies[agentProfilesPaletteID] = aiNever(aiNoAIConfig + "; " + aiNoPicker)
	aiSettingPolicies["agent_profiles"] = aiNever(aiNoAIConfig + "; how the outside agents start: model, permission mode and env")
}
