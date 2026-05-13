package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/prompt"
	"termocode/internal/toast"
)

// settingsConfig is the persisted shape of ~/.config/termocode/config.json.
// The "theme" key is preserved verbatim (read-modify-write) so the
// settings UI never clobbers the user's color choice. New entries can be
// appended without breaking older binaries — json.Unmarshal silently
// ignores unknown keys and missing keys retain the zero value.
//
// Pointer fields keep "value not set" distinct from "value is the zero
// default". A bool *true is meaningful; a missing key remains nil so the
// caller can apply a sane fallback (e.g. tab_size defaults to 4, not 0).
type settingsConfig struct {
	Theme      string `json:"theme,omitempty"`
	FontDelta  *int   `json:"font_delta,omitempty"`
	AutoSave   *bool  `json:"auto_save,omitempty"`
	WordWrap   *bool  `json:"word_wrap,omitempty"`
	ShowHidden *bool  `json:"show_hidden,omitempty"`
	TabSize    *int   `json:"tab_size,omitempty"`
}

// settingsConfigPath returns the absolute path to config.json. We don't
// reuse theme.ConfigPath() at the import level because that would create
// an awkward dependency cycle once the settings UI is wired into the
// toast system; the rule is "this is the same file theme uses, do not
// introduce a second one".
func settingsConfigPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "config.json"), nil
}

// loadSettings reads config.json. Missing/corrupt = empty struct.
func loadSettings() settingsConfig {
	path, err := settingsConfigPath()
	if err != nil {
		return settingsConfig{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return settingsConfig{}
		}
		return settingsConfig{}
	}
	var cfg settingsConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return settingsConfig{}
	}
	return cfg
}

// saveSettings writes the current settings struct to config.json,
// merging into whatever's already on disk (so the theme key set by
// theme.SaveThemeID stays intact even if it isn't represented in our
// struct because of a version skew). Unknown JSON keys in the existing
// file are passed through unchanged via a generic map first, then
// overlaid with the typed values.
func saveSettings(cfg settingsConfig) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	merged := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	if cfg.Theme != "" {
		merged["theme"] = cfg.Theme
	}
	if cfg.FontDelta != nil {
		merged["font_delta"] = *cfg.FontDelta
	}
	if cfg.AutoSave != nil {
		merged["auto_save"] = *cfg.AutoSave
	}
	if cfg.WordWrap != nil {
		merged["word_wrap"] = *cfg.WordWrap
	}
	if cfg.ShowHidden != nil {
		merged["show_hidden"] = *cfg.ShowHidden
	}
	if cfg.TabSize != nil {
		merged["tab_size"] = *cfg.TabSize
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// settingsRow describes one editable setting for the settings UI.
type settingsRow struct {
	ID    string // stable id used in picker.Item.ID
	Label string // human-readable name shown in the picker
	Kind  string // "string", "int", "bool"
}

// settingsRows is the canonical list of settings the UI exposes. Order is
// the row order in the picker.
var settingsRows = []settingsRow{
	{ID: "setting-theme", Label: "theme", Kind: "string"},
	{ID: "setting-font-delta", Label: "font_delta", Kind: "int"},
	{ID: "setting-auto-save", Label: "auto_save", Kind: "bool"},
	{ID: "setting-word-wrap", Label: "word_wrap", Kind: "bool"},
	{ID: "setting-show-hidden", Label: "show_hidden", Kind: "bool"},
	{ID: "setting-tab-size", Label: "tab_size", Kind: "int"},
}

// findSettingsRow looks up a row by ID. Returns the zero value + false on
// miss.
func findSettingsRow(id string) (settingsRow, bool) {
	for _, r := range settingsRows {
		if r.ID == id {
			return r, true
		}
	}
	return settingsRow{}, false
}

// openSettingsPicker pops the dedicated Settings overlay. Unlike the
// universal picker (used for files, themes, snippets, …) Settings has its
// own structured layout: header + grouped rows + aligned label/value, see
// settings_modal.go for the renderer. Row data still comes from
// settingsRows so adding a new setting only requires touching that list.
func (m *Model) openSettingsPicker() tea.Cmd {
	m.settingsModal = newSettingsModal()
	m.settingsModal.SetSize(m.w, m.h)
	m.settingsModalOpen = true
	// Close any picker that might be lingering open so we don't double-render.
	m.pickerOpen = false
	return nil
}

// currentSettingValue returns the human-readable representation of a
// setting's current value, used as the picker row hint.
func currentSettingValue(r settingsRow, cfg settingsConfig) string {
	switch r.ID {
	case "setting-theme":
		if cfg.Theme == "" {
			return "vscode-dark-plus"
		}
		return cfg.Theme
	case "setting-font-delta":
		if cfg.FontDelta == nil {
			return "-2"
		}
		return strconv.Itoa(*cfg.FontDelta)
	case "setting-auto-save":
		if cfg.AutoSave == nil {
			return "false"
		}
		return strconv.FormatBool(*cfg.AutoSave)
	case "setting-word-wrap":
		if cfg.WordWrap == nil {
			return "false"
		}
		return strconv.FormatBool(*cfg.WordWrap)
	case "setting-show-hidden":
		if cfg.ShowHidden == nil {
			return "false"
		}
		return strconv.FormatBool(*cfg.ShowHidden)
	case "setting-tab-size":
		if cfg.TabSize == nil {
			return "4"
		}
		return strconv.Itoa(*cfg.TabSize)
	}
	return ""
}

// onSettingsRowSelected opens a prompt for the chosen row's new value.
// The prompt's submit handler is dispatched via promptKindSetting.
func (m *Model) onSettingsRowSelected(id string) tea.Cmd {
	row, ok := findSettingsRow(id)
	if !ok {
		return nil
	}
	cfg := loadSettings()
	current := currentSettingValue(row, cfg)
	hint := row.Label
	switch row.Kind {
	case "bool":
		hint += " (true / false)"
	case "int":
		hint += " (integer)"
	}
	m.prompt = prompt.New("Setting", hint+":", current)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindSetting
	m.settingPromptID = id
	return nil
}

// applySettingValue is invoked from handlePromptSubmit when the user
// confirms a new value for the active setting. Validates by Kind, persists
// to config.json, and applies live (calls the appropriate toggleX helper
// or runs an Ex command on nvim) so the user sees the change immediately.
func (m *Model) applySettingValue(value string) tea.Cmd {
	row, ok := findSettingsRow(m.settingPromptID)
	if !ok {
		return nil
	}
	cfg := loadSettings()
	var toastCmd tea.Cmd
	switch row.ID {
	case "setting-theme":
		// Validate the theme exists before persisting; otherwise the
		// next launch would silently fall back to the default and the
		// user wouldn't know why their choice was rejected.
		// Lookup uses the same package the picker does.
		m.applyTheme(value) // applyTheme is best-effort: bad ids fall back to the default and the user sees stale palette.
		cfg.Theme = value
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "theme", value)
	case "setting-font-delta":
		n, err := strconv.Atoi(value)
		if err != nil {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "font_delta: not an integer")
			return toastCmd
		}
		cfg.FontDelta = &n
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("font_delta = %d (restart to apply)", n))
	case "setting-auto-save":
		b, err := parseBoolLoose(value)
		if err != nil {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "auto_save: expected true/false")
			return toastCmd
		}
		cfg.AutoSave = &b
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		// Drive the existing toggle helper to keep state in sync. Only
		// flip if we'd actually change m.autoSaveOn — toggleAutoSave
		// inverts unconditionally.
		if m.autoSaveOn != b {
			return m.toggleAutoSave()
		}
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("auto_save = %v", b))
	case "setting-word-wrap":
		b, err := parseBoolLoose(value)
		if err != nil {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "word_wrap: expected true/false")
			return toastCmd
		}
		cfg.WordWrap = &b
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		// Sync the live nvim state with the new setting WITHOUT routing
		// through toggleWordWrap (which would push a duplicate toast on
		// top of the "word_wrap = …" toast we emit below).
		if m.nvim != nil {
			cur, _ := m.nvim.EvalLuaString(`return vim.wo.wrap and 'on' or 'off'`)
			if (cur == "on") != b {
				cmd := "setlocal nowrap"
				if b {
					cmd = "setlocal wrap"
				}
				_ = m.nvim.Command(cmd)
			}
		}
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("word_wrap = %v", b))
	case "setting-show-hidden":
		b, err := parseBoolLoose(value)
		if err != nil {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "show_hidden: expected true/false")
			return toastCmd
		}
		cfg.ShowHidden = &b
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		// Sync explorer state via the existing toggle.
		if m.explorer.HiddenShown() != b {
			return m.toggleHiddenFiles()
		}
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("show_hidden = %v", b))
	case "setting-tab-size":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 16 {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "tab_size: expected integer 1..16")
			return toastCmd
		}
		cfg.TabSize = &n
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		if m.nvim != nil {
			_ = m.nvim.Command(fmt.Sprintf("set tabstop=%d shiftwidth=%d", n, n))
		}
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("tab_size = %d", n))
	}
	return toastCmd
}

// parseBoolLoose accepts the obvious truthy/falsy spellings users might
// type — strconv.ParseBool would reject "yes" / "no" / "on" / "off".
func parseBoolLoose(s string) (bool, error) {
	switch s {
	case "true", "True", "TRUE", "1", "yes", "y", "on":
		return true, nil
	case "false", "False", "FALSE", "0", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("unrecognised bool: %q", s)
}

// applyStartupSettings reads config.json and applies any per-launch
// settings (auto_save, word_wrap, show_hidden, tab_size) at startup.
// Theme is already handled by theme.LoadAndApply(); font_delta is read
// before the TUI even starts (cmd/termocode/main.go), so we don't
// re-apply it here. Best-effort — failures are logged but never block
// startup.
func (m *Model) applyStartupSettings() {
	cfg := loadSettings()
	if cfg.AutoSave != nil && *cfg.AutoSave && !m.autoSaveOn {
		_ = m.toggleAutoSave()
	}
	if cfg.WordWrap != nil && *cfg.WordWrap && m.nvim != nil {
		// EvalLuaString isn't safe here yet (nvim may still be attaching);
		// this helper is invoked AFTER ReadyMsg so it's safe.
		// Apply persisted word_wrap setting silently on startup — no
		// toast (we don't want a "Word wrap enabled" pop on every launch).
		cur, _ := m.nvim.EvalLuaString(`return vim.wo.wrap and 'on' or 'off'`)
		if cur != "on" {
			_ = m.nvim.Command("setlocal wrap")
		}
	}
	if cfg.ShowHidden != nil && *cfg.ShowHidden && !m.explorer.HiddenShown() {
		_ = m.toggleHiddenFiles()
	}
	if cfg.TabSize != nil && *cfg.TabSize > 0 && m.nvim != nil {
		_ = m.nvim.Command(fmt.Sprintf("set tabstop=%d shiftwidth=%d", *cfg.TabSize, *cfg.TabSize))
	}
}
