package app

import (
	"fmt"
	"strings"

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
// confirms a new binding string. Validates it with keymap.ValidateKey,
// persists to keymap.json, and rebuilds m.keys so
// the change takes effect without restart.
func (m *Model) applyKeybinding(value string) tea.Cmd {
	if m.keybindingPromptAction == "" {
		return nil
	}
	var toastCmd tea.Cmd
	// Reject malformed keys early. This is the same gate keymap.LoadOverrides
	// applies on read; doing it on write too ensures the persisted file
	// stays clean and the user gets immediate feedback. Any valid key is
	// fine, even one no default binding uses (e.g. ctrl+k).
	value = strings.TrimSpace(value)
	if err := keymap.ValidateKey(value); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Invalid key", err.Error())
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

// keymapProblemsToast warns once about keymap.json entries that were
// dropped at startup (malformed key, unknown action) and logs each one to
// errors.log. nil when there is nothing to report.
func (m *Model) keymapProblemsToast() tea.Cmd {
	probs := m.keymapProblems
	m.keymapProblems = nil
	if len(probs) == 0 {
		return nil
	}
	for _, p := range probs {
		recordError("keymap.json: " + p)
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, keymapProblemsTitle(len(probs)), strings.Join(probs, "\n"))
	return c
}

// keymapProblemsTitle is the toast title for n dropped keymap.json entries.
func keymapProblemsTitle(n int) string {
	if n == 1 {
		return "keymap.json: 1 entry ignored"
	}
	return fmt.Sprintf("keymap.json: %d entries ignored", n)
}
