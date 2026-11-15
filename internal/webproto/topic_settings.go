package webproto

// The settings data (WEB-46, web design 2.9): the laptop's config.json
// keys that are on the hub's allowlist. No AI credential, secret, web,
// MCP trust or auto-approve key is ever listed. settings.set changes one
// listed key as the laptop's Settings row does (the phone's Settings
// page); a key off the list is refused. The delta is the whole list
// again, sent when the laptop's settings changed.

func init() {
	registerTopic("settings", SettingsSnap{}, SettingsSnap{})
	registerResult(OpSettingsSet, SettingItem{})
}

// OpSettingsSet changes one allow-listed setting.
const OpSettingsSet = "settings.set"

var _ = func() bool {
	opTable = append(opTable, Policy{Op: OpSettingsSet, New: newOf[SettingsSetArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2})
	return true
}()

// MaxSettingValue caps a value settings.set takes.
const MaxSettingValue = 256

// SettingsSetArgs names a setting by its config.json key and gives its
// new value as the Settings row takes it ("true", "4", "ascii").
type SettingsSetArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (a *SettingsSetArgs) validate(int) error {
	if a.Key == "" || len(a.Key) > 64 {
		return invalid("bad setting key")
	}
	for _, r := range a.Key {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return invalid("bad setting key")
		}
	}
	if len(a.Value) > MaxSettingValue || hasControl(a.Value) {
		return invalid("bad setting value")
	}
	return nil
}

// MaxSettings caps the settings listed.
const MaxSettings = 200

// SettingsSnap is the settings topic.
type SettingsSnap struct {
	Items []SettingItem `json:"items"`
}

// SettingItem is one setting with its value as the laptop uses it.
type SettingItem struct {
	Key         string `json:"key"`   // the config.json key
	Group       string `json:"group"` // the Settings group ("Appearance", "Editor" …)
	Type        string `json:"type" enum:"bool|int|number|enum|string"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	Description string `json:"description,omitempty"`
	// Choices are an enum setting's values, in the Settings row's order.
	Choices []string `json:"choices,omitempty"`
}
