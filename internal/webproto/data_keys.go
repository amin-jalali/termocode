package webproto

// The laptop's key bindings from the web (WEBP-7; the laptop's
// Preferences: Customize Keybindings). The page lists every laptop
// action with its keys; binding a key is the user's own preference,
// saved at once in the laptop's keymap.json, as the laptop's picker
// saves it. A key another action has is only taken with Replace: the
// first answer names that action, so the page can ask.

// Op names.
const (
	OpKeysList = "keys.list"
	OpKeysBind = "keys.bind"
)

// MaxKeyName caps a key string ("ctrl+alt+pgdown").
const MaxKeyName = 32

// KeyBindArgs binds Key to Action (a laptop action name: "PickTheme").
type KeyBindArgs struct {
	Action  string `json:"action"`
	Key     string `json:"key"`
	Replace bool   `json:"replace,omitempty"`
}

func (a *KeyBindArgs) validate(int) error {
	if a.Action == "" || len(a.Action) > 64 {
		return invalid("bad action")
	}
	for _, r := range a.Action {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return invalid("bad action")
		}
	}
	if a.Key == "" || len(a.Key) > MaxKeyName || hasControl(a.Key) {
		return invalid("bad key")
	}
	return nil
}

// KeyBinding is one laptop action and the keys bound to it, sorted.
type KeyBinding struct {
	Action string   `json:"action"`
	Keys   []string `json:"keys"`
}

// KeyBindings answers keys.list, sorted by action name.
type KeyBindings struct {
	Items []KeyBinding `json:"items"`
}

// KeyBound answers keys.bind: Saved, or Conflict names the action that
// has the key now (nothing saved; bind again with Replace to take it).
type KeyBound struct {
	Saved    bool   `json:"saved,omitempty"`
	Conflict string `json:"conflict,omitempty"`
}

var opKeyRows = []Policy{
	{Op: OpKeysList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKeysBind, New: newOf[KeyBindArgs](), Role: RoleController, Rate: perSec(4), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opKeyRows...); return true }()

func init() {
	registerResult(OpKeysList, KeyBindings{})
	registerResult(OpKeysBind, KeyBound{})
}
