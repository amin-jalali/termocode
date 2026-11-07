package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Effort dial (CMP-24): AI: Effort Dial… and AI: Next Effort Mode. One
// pick sets the agent profile (stored for this workspace, like AI:
// Switch Profile…) and the chat's reasoning level (stored with the
// chat, like AI: Next Reasoning Level). The modes and what they set
// come from ai.Settings.EffortDial (ai_dial changes them). The header
// keeps showing the profile chip and the "think <level>" chip, so the
// dial hides nothing. The picker also has the plan and fast toggles.

const pickerKindAIDial pickerKindEnum = 4240

// The picker's toggle rows (not modes).
const (
	aiDialPlanID = "·plan"
	aiDialFastID = "·fast"
)

// aiDialMode is the mode the chat is on now ("" when the profile and
// level match no mode).
func (m Model) aiDialMode() string {
	if m.ai == nil {
		return ""
	}
	dial, _ := m.ai.settings.EffortDial()
	lvl, _ := m.ai.shownReasoning()
	return ai.DialModeOf(dial, m.ai.activeProfile().Name, lvl)
}

// applyAIDial switches to mode: its profile (an untrusted project
// profile shows the trust card first and nothing changes yet), then
// its reasoning level for this chat.
func (m *Model) applyAIDial(mode string) tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	dial, problems := s.settings.EffortDial()
	for _, p := range problems {
		recordError("[ai] " + p)
	}
	d, ok := dial[ai.NormalizeDialMode(mode)]
	if !ok {
		return m.aiInfo("No effort mode " + mode)
	}
	name := d.Profile
	if name == "" {
		name = s.activeProfile().Name
	}
	p, ok := s.profiles.Get(name)
	if !ok {
		return m.aiInfo("Effort " + mode + ": no profile " + name + " (see ai_dial in config.json)")
	}
	if m.aiProfileTrust(p) != aiTrustUse {
		return m.askAIProfileTrust(p, aiTrustAfterSwitch, "")
	}
	m.storeAIProfile(name)
	if l := ai.NormalizeReasoning(d.Reasoning); l != "" {
		s.conv.Reasoning = l
	} else {
		s.conv.Reasoning = aiReasoningAuto
	}
	if len(s.conv.Messages) > 0 {
		if err := s.store.Save(&s.conv); err != nil {
			recordError("[ai] save chat: " + err.Error())
		}
	}
	msg := "Effort " + mode + ": " + ai.DialSummary(ai.DialSetting{Profile: name, Reasoning: d.Reasoning})
	if mode == ai.DialHigh || mode == ai.DialUltra {
		msg += " (more thinking costs more tokens)"
	}
	return m.aiInfo(msg)
}

// cycleAIDial is AI: Next Effort Mode: low → medium → high → ultra →
// low (a chat on no mode starts at low).
func (m *Model) cycleAIDial() tea.Cmd {
	return m.applyAIDial(ai.NextDialMode(m.aiDialMode()))
}

// openAIDialPicker is AI: Effort Dial…: the four modes with what each
// sets, then the plan and fast toggles.
func (m *Model) openAIDialPicker() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	dial, _ := s.settings.EffortDial()
	cur := m.aiDialMode()
	var items []picker.Item
	for _, mode := range ai.DialModes {
		title := mode
		if mode == cur {
			title += " (now)"
		}
		items = append(items, picker.Item{ID: mode, Title: title, Hint: ai.DialSummary(dial[mode])})
	}
	active := s.activeProfile().Name
	onOff := func(name string) string {
		if active == name {
			return "on: pick to go back to " + ai.ProfileDefault
		}
		return "off: pick to turn on"
	}
	items = append(items,
		picker.Item{ID: aiDialPlanID, Title: "Plan first (toggle)", Hint: "profile " + ai.ProfilePlan + " · " + onOff(ai.ProfilePlan)},
		picker.Item{ID: aiDialFastID, Title: "Fast (toggle)", Hint: "profile " + ai.ProfileFast + " · " + onOff(ai.ProfileFast)})
	m.picker = picker.NewItems(" "+aiSpark()+" Effort ", items)
	if cur != "" {
		m.picker.SetCursorID(cur)
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIDial
	return nil
}

// onAIDialPicked runs a dial picker row.
func (m *Model) onAIDialPicked(id string) tea.Cmd {
	switch id {
	case aiDialPlanID:
		return m.toggleAIDialProfile(ai.ProfilePlan)
	case aiDialFastID:
		return m.toggleAIDialProfile(ai.ProfileFast)
	}
	return m.applyAIDial(id)
}

// toggleAIDialProfile turns profile name on, or back to default when it
// is on; the chat's reasoning level stays.
func (m *Model) toggleAIDialProfile(name string) tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	if s.activeProfile().Name == name {
		return m.selectAIProfile(ai.ProfileDefault)
	}
	return m.selectAIProfile(name)
}
