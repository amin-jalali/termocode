package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// openKeybindingPicker pops a picker listing every action with its
// current binding. Each row shows "<action name>  <current binding>".
func (m *Model) openKeybindingPicker() tea.Cmd {
	bindings := m.keys.Bindings()
	// Reverse the binding map so we can quickly look up Action -> key.
	// Multiple keys may map to the same Action (e.g. ctrl+t and f9 both
	// toggle the terminal); we surface only the first encountered key
	// in the picker, but the user can still rebind any action.
	actionToKey := map[keymap.Action]string{}
	for k, a := range bindings {
		if _, ok := actionToKey[a]; !ok {
			actionToKey[a] = k
		}
	}
	actions := keymap.AllActions()
	items := make([]picker.Item, 0, len(actions))
	for _, a := range actions {
		key := actionToKey[a]
		if key == "" {
			key = "<unbound>"
		}
		items = append(items, picker.Item{
			ID:    "kb-" + keymap.ActionName(a),
			Title: keymap.ActionName(a),
			Hint:  key,
		})
	}
	m.picker = picker.NewItems(" Customize Keybindings ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindKeybinding
	return m.picker.Init()
}

// onKeybindingRowSelected opens a prompt for the new binding string.
func (m *Model) onKeybindingRowSelected(id string) tea.Cmd {
	const prefix = "kb-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return nil
	}
	actionName := id[len(prefix):]
	// Show the current binding (if any) as the prompt's initial value
	// so the user can edit-in-place rather than retype from scratch.
	current := ""
	for k, a := range m.keys.Bindings() {
		if keymap.ActionName(a) == actionName {
			current = k
			break
		}
	}
	m.prompt = prompt.New("Customize Keybinding", actionName+" (e.g. ctrl+x, f5, alt+enter):", current)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindKeybinding
	m.keybindingPromptAction = actionName
	return nil
}

// applyKeybinding is invoked from handlePromptSubmit when the user
// confirms a new binding string. Validates against keymap.Default()'s
// recognised key set, persists to keymap.json, and rebuilds m.keys so
// the change takes effect without restart.
func (m *Model) applyKeybinding(value string) tea.Cmd {
	if m.keybindingPromptAction == "" {
		return nil
	}
	var toastCmd tea.Cmd
	// Reject unknown keys early. This is the same gate keymap.LoadOverrides
	// applies on read; doing it on write too ensures the persisted file
	// stays clean and the user gets immediate feedback.
	if !keymap.IsKnownKey(value) {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Unknown key", value)
		return toastCmd
	}
	// Find the Action for this name. We rely on the same name table
	// LoadOverrides uses, so a roundtrip via SaveOverrides + LoadOverrides
	// is lossless.
	var action keymap.Action
	for _, a := range keymap.AllActions() {
		if keymap.ActionName(a) == m.keybindingPromptAction {
			action = a
			break
		}
	}
	if action == keymap.ActionNone {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Unknown action", m.keybindingPromptAction)
		return toastCmd
	}
	// Conflict check: if the chord already belongs to another action,
	// ask before stealing it. The confirm's Replace button routes back to
	// commitKeybinding via confirmKindKeybindingConflict.
	if owner, clash := m.keys.Conflict(value, action); clash {
		m.extras.kbPendingKey = value
		m.extras.kbPendingAction = action
		m.confirm = confirm.New(
			"Key already in use",
			fmt.Sprintf("%s is already bound to %s.\n\nReplace it with %s?\n(%s will lose this key.)",
				value, keymap.ActionName(owner), m.keybindingPromptAction, keymap.ActionName(owner)),
			[]confirm.Button{
				{ID: "replace", Title: "Replace", Style: confirm.StyleDestructive},
				{ID: "cancel", Title: "Cancel"},
			},
		)
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindKeybindingConflict
		return nil
	}
	return m.commitKeybinding(value, action)
}

// onKeybindingConflictConfirm handles the Replace / Cancel buttons of the
// "Key already in use" dialog opened by applyKeybinding.
func (m *Model) onKeybindingConflictConfirm(id string) tea.Cmd {
	key, action := m.extras.kbPendingKey, m.extras.kbPendingAction
	m.extras.kbPendingKey, m.extras.kbPendingAction = "", keymap.ActionNone
	if id != "replace" || key == "" || action == keymap.ActionNone {
		m.keybindingPromptAction = ""
		return nil
	}
	return m.commitKeybinding(key, action)
}

// commitKeybinding persists value -> action to keymap.json and rebuilds
// m.keys so the change is live without a restart.
func (m *Model) commitKeybinding(value string, action keymap.Action) tea.Cmd {
	var toastCmd tea.Cmd
	// Persist + apply atomically. The override file is read-modify-write,
	// so other entries the user previously customised stay intact.
	entry := map[string]keymap.Action{value: action}
	if err := keymap.SaveOverrides("", entry); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Keymap error", err.Error())
		return toastCmd
	}
	overrides := keymap.LoadOverrides("")
	m.keys = keymap.Default().MergeInto(overrides)
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, keymap.ActionName(action), value)
	m.keybindingPromptAction = ""
	return toastCmd
}
