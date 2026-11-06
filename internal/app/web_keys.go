package app

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's key bindings from the web (WEBP-7): Preferences:
// Customize Keybindings on the page. A key is checked and saved as the
// laptop's picker does it (keymap.ValidateKey, keymap.json) and the
// laptop uses it at once.

func init() {
	webOps[webproto.OpKeysList] = (*Model).webKeysList
	webOps[webproto.OpKeysBind] = (*Model).webKeysBind
}

func (m *Model) webKeysList(call *webCall) tea.Cmd {
	byAction := map[keymap.Action][]string{}
	for k, a := range m.keys.Bindings() {
		byAction[a] = append(byAction[a], k)
	}
	out := webproto.KeyBindings{Items: []webproto.KeyBinding{}}
	for _, a := range keymap.AllActions() {
		keys := byAction[a]
		sort.Strings(keys)
		if keys == nil {
			keys = []string{}
		}
		out.Items = append(out.Items, webproto.KeyBinding{Action: keymap.ActionName(a), Keys: keys})
	}
	call.reply(out)
	return nil
}

func (m *Model) webKeysBind(call *webCall) tea.Cmd {
	a := call.args.(*webproto.KeyBindArgs)
	action := keymap.ActionNone
	for _, x := range keymap.AllActions() {
		if keymap.ActionName(x) == a.Action {
			action = x
		}
	}
	if action == keymap.ActionNone {
		call.fail(webproto.CodeNotFound, "The laptop has no action of that name.")
		return nil
	}
	if err := keymap.ValidateKey(a.Key); err != nil {
		call.fail(webproto.CodeBadRequest, "The laptop cannot read that key: use a character, f1-f24 or a named key, with ctrl+, alt+ or shift+.")
		return nil
	}
	if owner, clash := m.keys.Conflict(a.Key, action); clash && !a.Replace {
		call.reply(webproto.KeyBound{Conflict: keymap.ActionName(owner)})
		return nil
	}
	if err := keymap.SaveOverrides("", map[string]keymap.Action{a.Key: action}); err != nil {
		call.fail(webproto.CodeInternal, "The key was not saved.")
		return nil
	}
	m.keys = keymap.Default().MergeInto(keymap.LoadOverrides(""))
	call.reply(webproto.KeyBound{Saved: true})
	return nil
}
