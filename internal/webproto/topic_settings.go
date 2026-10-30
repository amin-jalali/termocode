package webproto

// The settings data (WEB-46, web design 2.9): a read-only view of the
// laptop's config.json keys that are on the hub's allowlist. No AI
// credential, secret, web, MCP trust or auto-approve key is ever listed,
// and the web cannot change a setting. The delta is the whole list
// again, sent when the laptop's settings changed.

func init() {
	registerTopic("settings", SettingsSnap{}, SettingsSnap{})
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
}
