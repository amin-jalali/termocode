package app

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The profile step form (AH-5): New profile from the active one, Edit a
// profile, both without JSON. The form (internal/profileform) checks the
// fields with ai.ValidateProfile; Save… shows the preview card (U23) and
// only its Save writes profiles.json (ai.SaveProfile: a safe
// read-modify-write that keeps other profiles and keys). The form never
// sees a key: a slot shows ✓ / ✗ only.

const (
	confirmKindAIProfileFormStep confirmKindEnum = 1010 + iota
	confirmKindAIProfileFormDelete
	confirmKindAIProfileFormDiscard
)

const pickerKindAIProfileEdit pickerKindEnum = 1010

// aiFormSlots are the model slots the form offers, main / quick / local
// first. KeyOK is a bare yes / no (the key itself stays in the store).
func (m *Model) aiFormSlots() []profileform.Slot {
	s := m.aiS()
	var names []string
	for n := range s.slotKnown {
		if n != ai.SlotMain && n != ai.SlotQuick && n != ai.SlotLocal {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	names = append([]string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal}, names...)
	var out []profileform.Slot
	for _, n := range names {
		if s.slotKnown != nil && !s.slotKnown[n] {
			continue
		}
		cfg, found := s.aiSlotConfig(n)
		ok := found && cfg.Configured() // a yes / no; the key is never read
		out = append(out, profileform.Slot{Name: n, KeyOK: ok})
	}
	return out
}

// openAIProfileForm opens the form on p under name. editing: p is one of
// the user's own profiles and Save replaces it.
func (m *Model) openAIProfileForm(name string, p ai.Profile, editing bool) tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	taken := map[string]bool{}
	for _, n := range s.profiles.Names() {
		if q, _ := s.profiles.Get(n); q.Source == ai.ProfileSourceUser {
			taken[n] = true
		}
	}
	title := "New profile"
	if editing {
		title = "Edit profile " + name
	}
	m.profileForm = profileform.New(name, p, profileform.Config{
		Title: title, Slots: m.aiFormSlots(), ParallelMax: s.settings.Parallel(), Taken: taken, Editing: editing,
		CLIs: s.cliStates,
	})
	m.profileForm.SetSize(m.w, m.h)
	m.profileFormOpen, m.profileFormParked = true, false
	return aiCLICheckCmd() // Claude Code / Codex: installed, logged in? (CS-5)
}

// openAIProfileNewForm is New profile from <active>…: a copy to change.
func (m *Model) openAIProfileNewForm() tea.Cmd {
	p := m.aiS().activeProfile()
	return m.openAIProfileForm(m.freeProfileName("my-"+p.Name), p, false)
}

// openAIProfileEditForm is Edit <name>…: the user's own profile in place;
// a built-in or project profile becomes a copy under a new name.
func (m *Model) openAIProfileEditForm(name string) tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	p, ok := s.profiles.Get(name)
	if !ok {
		return m.aiInfo("No profile " + name)
	}
	if p.Source == ai.ProfileSourceUser {
		return m.openAIProfileForm(name, p, true)
	}
	return m.openAIProfileForm(m.freeProfileName("my-"+name), p, false)
}

// freeProfileName is base, or base-2, base-3 … when the name is taken.
func (m *Model) freeProfileName(base string) string {
	name := base
	for n := 2; ; n++ {
		if _, taken := m.aiS().profiles.Get(name); !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, n)
	}
}

// openAIProfileEditPicker is AI: Edit Profile…: which profile to change.
func (m *Model) openAIProfileEditPicker() tea.Cmd {
	s := m.aiS()
	s.reloadProfiles()
	var items []picker.Item
	box, _ := m.aiProfilePickerWidth()
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		h := "yours · edit in place"
		if p.Source != ai.ProfileSourceUser {
			h = "edit makes a copy under a new name"
		}
		items = append(items, picker.Item{ID: n, Title: n, Hint: h})
	}
	m.picker = picker.NewItems(" "+aiSpark()+" Edit Profile ", items)
	m.picker.SetWidth(min(box, 80))
	m.picker.SetCursorID(s.activeProfile().Name)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIProfileEdit
	return nil
}

// closeAIProfileForm closes the form (nothing was written).
func (m *Model) closeAIProfileForm() { m.profileFormOpen = false }

// handleAIProfileFormMsg routes what the form says. ok=false: not ours.
func (m *Model) handleAIProfileFormMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case profileform.SaveMsg:
		return m.aiProfileFormSave(), true
	case profileform.CLIActionMsg:
		return m.onProfileFormCLIAction(msg), true
	case profileform.CancelMsg:
		if !msg.Dirty {
			m.closeAIProfileForm()
			return nil, true
		}
		m.askRisky("Discard your changes?", "The profile file stays as it is.", confirmKindAIProfileFormDiscard,
			confirm.Button{ID: "keep", Title: "Keep editing"},
			confirm.Button{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive})
		return nil, true
	case profileform.RemoveStepMsg:
		m.profileFormStep = msg.Index
		m.askRisky(fmt.Sprintf("Remove step %q?", msg.Title),
			"The step and its prompt leave the profile when you save. Cancel in the form to undo.",
			confirmKindAIProfileFormStep,
			confirm.Button{ID: "keep", Title: "Keep step"},
			confirm.Button{ID: "remove", Title: "Remove", Style: confirm.StyleDestructive})
		return nil, true
	case profileform.DeleteMsg:
		m.askRisky(fmt.Sprintf("Delete profile %q?", m.profileForm.OrigName()),
			"It leaves profiles.json now. Other profiles and settings stay.",
			confirmKindAIProfileFormDelete,
			confirm.Button{ID: "keep", Title: "Keep profile"},
			confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
		return nil, true
	case profileform.KeysMsg:
		return m.openKeyCard("Profile form keys", keyRows(
			[]string{"Moving"},
			[]string{"Tab / ↓", "next field"},
			[]string{"Shift+Tab / ↑", "previous field"},
			[]string{"Editing"},
			[]string{"Enter", "edit a text field; cycle a choice"},
			[]string{"← →", "change a choice or the planner box"},
			[]string{"Esc", "stop editing (Esc again: cancel)"},
			[]string{"Steps"},
			[]string{"a", "add a step"},
			[]string{"K / J", "move the step up / down"},
			[]string{"x", "remove the step (asks first)"},
			[]string{"?", "this card"},
		), help.Loc{Page: "guides/ai-profiles.md", Anchor: "create-and-edit-profiles"}, nil), true
	}
	return nil, false
}

// askRisky opens a confirm card with the safe answer first.
func (m *Model) askRisky(title, text string, kind confirmKindEnum, safe confirm.Button, action confirm.Button) {
	m.confirm = confirm.Risky(title, text, safe, action)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
}

// answerAIProfileFormConfirm runs the cards the form opens. ok=false: not
// ours.
func (m *Model) answerAIProfileFormConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindAIProfileFormDiscard:
		if id == "discard" {
			m.closeAIProfileForm()
		}
		return nil, true
	case confirmKindAIProfileFormStep:
		if id == "remove" {
			m.profileForm.RemoveStep(m.profileFormStep)
		}
		return nil, true
	case confirmKindAIProfileFormDelete:
		if id != "delete" {
			return nil, true
		}
		name := m.profileForm.OrigName()
		path, err := ai.ProfilesPath()
		if err == nil {
			err = ai.DeleteProfile(path, name)
		}
		if err != nil {
			return m.aiToastErr("Delete profile", err), true
		}
		m.closeAIProfileForm()
		s := m.aiS()
		s.reloadProfiles()
		if s.profile == name {
			return tea.Batch(m.setAIProfile(ai.ProfileDefault), m.aiInfo("Profile "+name+" deleted")), true
		}
		return m.aiInfo("Profile " + name + " deleted"), true
	}
	return nil, false
}

// aiProfileFormSave shows the preview card (U23) for the form's profile.
func (m *Model) aiProfileFormSave() tea.Cmd {
	p := m.profileForm.Profile()
	cmd := m.askAIProfileSave(p, false)
	if pend := m.aiS().profileSave; pend != nil {
		pend.fromForm, pend.replace = true, m.profileForm.Replace()
	}
	return cmd
}

// aiProfileFormRoute sends keys, clicks and resizes to the open form.
func (m Model) routeToProfileForm(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.profileForm.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			for _, b := range m.profileForm.ButtonRects() {
				if msg.Y == b.Y && msg.X >= b.X && msg.X < b.X+b.W {
					m.probed("profileform:" + b.Title)
					return m, nil
				}
			}
			m.probed("profileform")
			return m, nil
		}
		var cmd tea.Cmd
		m.profileForm, cmd = m.profileForm.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.profileForm, cmd = m.profileForm.Update(msg)
	return m, cmd
}

// profileFormTargets are the form's buttons for the hit map.
func (m Model) profileFormTargets() []hitTarget {
	var out []hitTarget
	for _, b := range m.profileForm.ButtonRects() {
		out = append(out, hitTarget{ID: "profileform:" + b.Title, Label: b.Title, X: b.X, Y: b.Y, W: b.W, H: 1})
	}
	return out
}

// profileFormLayer draws the form under a card that sits on it (the
// preview, a question).
func (m Model) profileFormLayer(base string) string {
	if !m.profileFormOpen {
		return base
	}
	o := m.overlayOpts()
	o.At = true
	o.X, o.Y = m.profileForm.Origin()
	return modalOverlay(base, m.profileForm.Box(), m.w, m.h, &o)
}

// profileFormSaved is the form's part of the preview card's answer: the
// write, and the form closes only when it worked.
func (m *Model) profileFormSaved(pending *aiProfileSavePending) tea.Cmd {
	path, err := ai.ProfilesPath()
	if err == nil {
		err = ai.SaveProfile(path, pending.name, pending.profile, pending.replace)
	}
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Profile not saved", err.Error())
		return c
	}
	m.closeAIProfileForm()
	s := m.aiS()
	s.reloadProfiles()
	if _, ok := s.profiles.Get(pending.name); !ok {
		return m.aiProfileErrorsCmd()
	}
	return m.setAIProfile(pending.name)
}
