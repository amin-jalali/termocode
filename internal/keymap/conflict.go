package keymap

import "strings"

// Conflict reports which Action (if any) already owns `key` in k, other
// than `want`. The keybinding-customization UI calls it before writing an
// override so it can warn "ctrl+x is already bound to Cut" and let the
// user pick Replace or Cancel instead of silently stealing the chord.
//
// Returns (owner, true) when the key is bound to a different action;
// (ActionNone, false) when the key is free or already bound to `want`.
func (k KeyMap) Conflict(key string, want Action) (Action, bool) {
	owner, ok := k.bindings[strings.TrimSpace(key)]
	if !ok || owner == ActionNone || owner == want {
		return ActionNone, false
	}
	return owner, true
}
