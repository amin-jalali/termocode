package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Agent profile UI (U20 AP-5): the chat header chip "✦ <profile> ▾"
// and its picker, the Alt+Shift+P cycle, the palette entries, the
// project-profile trust card, AI: Edit Profiles and AI: New Profile from
// Current…. The active profile and the trust answers are kept per
// workspace in the session file.

const (
	pickerKindAIProfile pickerKindEnum = 1000 + iota
	pickerKindAIProfileTrust
)

const promptKindAIProfileNew promptKindEnum = 1000

const confirmKindAIProfileTrust confirmKindEnum = 1000

// The trust state of a project profile in this workspace.
const (
	aiTrustAsk     = ""        // never answered, or the profile changed since
	aiTrustUse     = "use"     // Use
	aiTrustRefused = "refused" // Not in this project
)

// What happens after the trust card's Use.
const (
	aiTrustAfterSwitch = iota // make it the active profile
	aiTrustAfterSend          // make it active and send the waiting message
	aiTrustAfterNone          // only store the answer (the trust picker)
)

// aiProfileTrustPending is the open trust card.
type aiProfileTrustPending struct {
	name, hash string
	after      int
	text       string // the chat message waiting for the answer
}

// aiProfileTrust is the user's answer for a project profile ("use" for
// every other source).
func (m *Model) aiProfileTrust(p ai.Profile) string {
	if p.Source != ai.ProfileSourceProject {
		return aiTrustUse
	}
	sess, h := loadSession(), ai.ProfileHash(p)
	switch {
	case sess.AIProfilesTrusted[p.Name] == h:
		return aiTrustUse
	case sess.AIProfilesRefused[p.Name] == h:
		return aiTrustRefused
	}
	return aiTrustAsk
}

// aiProfileChipLabel is the chat header chip: "✦ <profile> ▾".
func (m Model) aiProfileChipLabel() string {
	name := ai.ProfileDefault
	if m.ai != nil {
		name = m.ai.activeProfile().Name
	}
	return aiSpark() + " " + name + " " + aiFoldGlyph(true)
}

// aiRunActive: a pipeline run is going (the chip turns Accent).
func (m Model) aiRunActive() bool { return m.ai != nil && m.ai.run != nil }

// setAIProfile makes name the active profile, stores it for this
// workspace and says so in a toast.
func (m *Model) setAIProfile(name string) tea.Cmd {
	s := m.aiS()
	s.profile = name
	sess := loadSession()
	sess.AIProfile = name
	if name == ai.ProfileDefault {
		sess.AIProfile = ""
	}
	saveSession(sess)
	return m.aiInfo("Profile: " + name)
}

// selectAIProfile switches to name; an untrusted project profile shows
// the trust card first.
func (m *Model) selectAIProfile(name string) tea.Cmd {
	p, ok := m.aiS().profiles.Get(name)
	if !ok {
		return m.aiInfo("No profile " + name)
	}
	if m.aiProfileTrust(p) != aiTrustUse {
		return m.askAIProfileTrust(p, aiTrustAfterSwitch, "")
	}
	return m.setAIProfile(name)
}

// cycleAIProfile is Alt+Shift+P: the next profile after the active one
// (wrapping); project profiles refused in this workspace are skipped.
func (m *Model) cycleAIProfile() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	names := s.profiles.Names()
	cur := s.activeProfile().Name
	at := 0
	for i, n := range names {
		if n == cur {
			at = i
		}
	}
	for k := 1; k <= len(names); k++ {
		n := names[(at+k)%len(names)]
		if p, _ := s.profiles.Get(n); m.aiProfileTrust(p) == aiTrustRefused {
			continue
		}
		return tea.Batch(m.selectAIProfile(n), m.aiProfileErrorsCmd())
	}
	return nil
}

// aiProfileHint is a picker row's hint: where it comes from (a project
// profile's trust first, so a narrow picker still shows it), title,
// steps.
func (m *Model) aiProfileHint(p ai.Profile) string {
	var parts []string
	switch p.Source {
	case ai.ProfileSourceUser:
		parts = append(parts, "yours")
	case ai.ProfileSourceProject:
		switch m.aiProfileTrust(p) {
		case aiTrustUse:
			parts = append(parts, "project")
		case aiTrustRefused:
			parts = append(parts, "project, not used here")
		default:
			parts = append(parts, "project, asks first")
		}
	}
	if p.Title != "" {
		parts = append(parts, p.Title)
	}
	if n := len(p.Steps); n == 1 {
		parts = append(parts, "1 step")
	} else {
		parts = append(parts, fmt.Sprintf("%d steps", n))
	}
	return strings.Join(parts, " · ")
}

// openAIProfilePicker is AI: Switch Profile… and a click on the chip.
func (m *Model) openAIProfilePicker() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	var items []picker.Item
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		items = append(items, picker.Item{ID: n, Title: n, Hint: m.aiProfileHint(p)})
	}
	m.picker = picker.NewItems(" "+aiSpark()+" AI Profile ", items)
	m.picker.SetCursorID(s.activeProfile().Name)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIProfile
	return m.aiProfileErrorsCmd()
}

// openAIProfileTrustPicker is AI: Project Profiles Trust…: the project's
// profiles with their answers; a row opens the trust card.
func (m *Model) openAIProfileTrustPicker() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	var items []picker.Item
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		if p.Source != ai.ProfileSourceProject {
			continue
		}
		hint := "not asked yet"
		switch m.aiProfileTrust(p) {
		case aiTrustUse:
			hint = "used in this project"
		case aiTrustRefused:
			hint = "not used in this project"
		}
		items = append(items, picker.Item{ID: n, Title: n, Hint: hint})
	}
	if len(items) == 0 {
		return m.aiInfo("This project has no profiles (.termocode/ai-profiles.json)")
	}
	m.picker = picker.NewItems(" "+aiSpark()+" Project Profiles ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIProfileTrust
	return nil
}

// handleAIProfilePickerSelect routes the profile pickers. ok=false: not
// ours.
func (m *Model) handleAIProfilePickerSelect(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindAIProfile:
		return m.selectAIProfile(id), true
	case pickerKindAIProfileTrust:
		p, ok := m.aiS().profiles.Get(id)
		if !ok {
			return nil, true
		}
		return m.askAIProfileTrust(p, aiTrustAfterNone, ""), true
	}
	return nil, false
}

// aiProfilePromptHead is the first line of a step prompt for the trust
// card (at most 72 cells).
func aiProfilePromptHead(system string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(system), "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > 72 {
		line = string(r[:71]) + "…"
	}
	return line
}

// askAIProfileTrust opens the trust card of a project profile: its steps
// and the first line of each prompt (a repo can try to steer the agent).
func (m *Model) askAIProfileTrust(p ai.Profile, after int, text string) tea.Cmd {
	s := m.aiS()
	s.profTrust = &aiProfileTrustPending{name: p.Name, hash: ai.ProfileHash(p), after: after, text: text}
	var b strings.Builder
	b.WriteString("From this project's .termocode/ai-profiles.json. Its prompts steer the AI: use it only if you trust this project.\n")
	for i, st := range p.Steps {
		title := st.Title
		if title == "" {
			title = st.ID
		}
		tools := st.Tools.Preset
		if tools == "" {
			tools = agentdag.PresetAll
		}
		fmt.Fprintf(&b, "\n%d · %s · %s · tools %s", i+1, title, p.SlotFor(st), tools)
		if head := aiProfilePromptHead(st.System); head != "" {
			fmt.Fprintf(&b, "\n    “%s”", head)
		}
	}
	m.confirm = confirm.New("Use project profile "+p.Name+"?", b.String(), []confirm.Button{
		{ID: "use", Title: "Use", Style: confirm.StylePrimary},
		{ID: "deny", Title: "Not in this project", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIProfileTrust
	return nil
}

// answerAIProfileTrust stores the trust card's answer (Esc = no answer).
func (m *Model) answerAIProfileTrust(id string) tea.Cmd {
	s := m.aiS()
	p := s.profTrust
	s.profTrust = nil
	if p == nil || (id != "use" && id != "deny") {
		if p != nil && p.text != "" {
			m.aiPutBackInput(p.text)
		}
		return nil
	}
	sess := loadSession()
	if sess.AIProfilesTrusted == nil {
		sess.AIProfilesTrusted = map[string]string{}
	}
	if sess.AIProfilesRefused == nil {
		sess.AIProfilesRefused = map[string]string{}
	}
	if id == "deny" {
		sess.AIProfilesRefused[p.name] = p.hash
		delete(sess.AIProfilesTrusted, p.name)
		saveSession(sess)
		var cmd tea.Cmd
		if s.activeProfile().Name == p.name || p.after != aiTrustAfterNone {
			cmd = m.setAIProfile(ai.ProfileDefault)
		}
		if p.text != "" {
			m.aiPutBackInput(p.text)
		}
		return tea.Batch(cmd, m.aiInfo("Profile "+p.name+" is not used in this project"))
	}
	sess.AIProfilesTrusted[p.name] = p.hash
	delete(sess.AIProfilesRefused, p.name)
	saveSession(sess)
	switch p.after {
	case aiTrustAfterSwitch:
		return m.setAIProfile(p.name)
	case aiTrustAfterSend:
		s.profile = p.name
		return m.sendAIChat(p.text)
	}
	return m.aiInfo("Profile " + p.name + " can be used in this project")
}

// aiPutBackInput puts a message that was not sent back in the chat input.
func (m *Model) aiPutBackInput(text string) {
	s := m.aiS()
	if len(s.input) == 0 {
		s.input = []rune(text)
		s.caret = len(s.input)
	}
}

// editAIProfiles is AI: Edit Profiles: profiles.json in the editor,
// created with a working example when missing.
func (m *Model) editAIProfiles() tea.Cmd {
	path, err := ai.ProfilesPath()
	if err != nil {
		return m.aiToastErr("Profiles", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return m.aiToastErr("Profiles", err)
		}
		if err := os.WriteFile(path, []byte(ai.ExampleProfilesJSON), 0o644); err != nil {
			return m.aiToastErr("Profiles", err)
		}
	}
	m.openFileAtLine(path, 1)
	return m.aiInfo("Edit and save; the profile picker and Alt+Shift+P read the file again")
}

// openAIProfileNewPrompt is AI: New Profile from Current…: asks for the
// new name.
func (m *Model) openAIProfileNewPrompt() tea.Cmd {
	cur := m.aiS().activeProfile().Name
	m.prompt = prompt.New(aiSpark()+" New Profile", "Save profile "+cur+" as (letters, digits, - _ .):", "my-"+cur)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAIProfileNew
	return nil
}

// onAIProfileNewName writes the active profile under name in
// profiles.json and switches to it.
func (m *Model) onAIProfileNewName(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	path, err := ai.ProfilesPath()
	if err != nil {
		return m.aiToastErr("New profile", err)
	}
	s := m.aiS()
	if err := ai.SaveProfileAs(path, name, s.activeProfile()); err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Profile not saved", err.Error())
		return c
	}
	s.reloadProfiles()
	if _, ok := s.profiles.Get(name); !ok {
		return m.aiProfileErrorsCmd()
	}
	return m.setAIProfile(name)
}

// handleAIProfilePrompt routes the profile prompt. ok=false: not ours.
func (m *Model) handleAIProfilePrompt(value string) (tea.Cmd, bool) {
	if m.promptKind == promptKindAIProfileNew {
		return m.onAIProfileNewName(value), true
	}
	return nil, false
}

// editAIModelSlots is AI: Edit Model Slots: the AI config.json at its
// "ai_models" key (created when missing).
func (m *Model) editAIModelSlots() tea.Cmd {
	path, err := ai.ConfigPath()
	if err != nil {
		return m.aiToastErr("Model slots", err)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = []byte("{\n  \"ai_models\": {\n  }\n}\n")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return m.aiToastErr("Model slots", err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return m.aiToastErr("Model slots", err)
		}
	}
	line := 1
	if i := strings.Index(string(data), `"ai_models"`); i >= 0 {
		line += strings.Count(string(data[:i]), "\n")
	}
	m.openFileAtLine(path, line)
	return m.aiInfo(`Slots: "ai_models": {"gpt": {"provider": "openai", "model": "gpt-5-mini"}} (no keys here); save, then AI: Configure Provider… reloads`)
}

// aiProfileChipTok is the chip's colour: the AI accent (as a running
// step card) while a run is active, TextSecondary otherwise.
func (m Model) aiProfileChipTok() theme.Color256 {
	if m.aiRunActive() {
		return theme.AIAccent
	}
	return theme.TextSecondary
}
