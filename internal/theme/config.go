package theme

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// configFile is the JSON document persisted to ~/.config/termocode/config.json.
// Only the theme ID is stored today; new fields can be added without breaking
// older configs (json.Unmarshal silently ignores unknown keys, and missing keys
// retain their zero value).
type configFile struct {
	Theme string `json:"theme,omitempty"`
}

// ConfigPath returns the absolute path to the persisted config file. Honors
// $XDG_CONFIG_HOME, falls back to ~/.config.
func ConfigPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "config.json"), nil
}

// LoadThemeID reads the persisted theme ID. Returns "" when the file is
// missing or unreadable so callers can apply their default silently. Corrupt
// JSON is also treated as "missing" — we never want a malformed config file
// to prevent startup.
func LoadThemeID() string {
	path, err := ConfigPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}
		return ""
	}
	var cfg configFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ""
	}
	return cfg.Theme
}

// SaveThemeID persists the chosen theme ID, creating ~/.config/termocode/ if
// needed. Errors are returned but the caller is free to ignore them — a
// failed write just means the choice doesn't survive the next launch.
func SaveThemeID(id string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Read-modify-write so we don't clobber other config keys a future
	// version may have added.
	cfg := configFile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg.Theme = id
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// LoadAndApply reads the persisted theme ID and applies it. Returns the
// resolved NamedTheme so the caller can store its composite styles. When no
// config exists or the stored ID is unknown, falls back to VSCode Dark+.
func LoadAndApply() NamedTheme {
	id := LoadThemeID()
	if id == "" {
		id = "vscode-dark-plus"
	}
	t, _ := ApplyTheme(id)
	return t
}
