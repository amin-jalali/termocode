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

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/toast"
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
	// SearchMaxResults caps Find-in-Files results (0 = unlimited).
	SearchMaxResults *int `json:"search_max_results,omitempty"`
	// Display (display_settings.go). "" means auto.
	Icons      string `json:"icons,omitempty"`
	Animations string `json:"animations,omitempty"`
	Images     string `json:"images,omitempty"`
	// TerminalSkipShell adds ("Name") or removes ("-Name") actions whose
	// keys skip the focused terminal and reach termocode (default: Quit).
	TerminalSkipShell []string `json:"terminal_skip_shell,omitempty"`
	// PersistentSession: "auto" (on over SSH), "on" or "off" ("" = auto).
	// cmd/termocode reads it before the UI starts.
	PersistentSession string `json:"persistent_session,omitempty"`
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

// ConfigFontDelta returns the "font_delta" value from config.json, or nil
// when it is not set. cmd/termocode reads it before the TUI starts.
func ConfigFontDelta() *int {
	return loadSettings().FontDelta
}

// ConfigPersistentSession returns the "persistent_session" value from
// config.json ("" = auto). cmd/termocode reads it before the TUI starts.
func ConfigPersistentSession() string {
	return loadSettings().PersistentSession
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
	if cfg.SearchMaxResults != nil {
		merged["search_max_results"] = *cfg.SearchMaxResults
	}
	for k, v := range map[string]string{"icons": cfg.Icons, "animations": cfg.Animations, "images": cfg.Images, "persistent_session": cfg.PersistentSession} {
		if v != "" {
			merged[k] = v
		}
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
	// Docs: Default is the value used when the key is missing from
	// config.json; Desc is the one-line help for docs/reference/settings.md.
	Default string
	Desc    string
}

// settingsRows is the canonical list of settings the UI exposes. Order is
// the row order in the picker.
var settingsRows = []settingsRow{
	{ID: "setting-theme", Label: "theme", Kind: "string", Default: "vscode-dark-plus",
		Desc: "Color theme ID. Same as Preferences: Color Theme."},
	{ID: "setting-icons", Label: "icons", Kind: "enum", Default: "auto",
		Desc: "Icon set: nerd (Nerd Font glyphs), unicode (plain symbols), ascii, or auto (nerd on terminals that bundle Nerd symbols — WezTerm, Ghostty, kitty — ascii on TERM=linux or a non-UTF-8 locale, else unicode). TERMOCODE_ICON_MODE wins over it."},
	{ID: "setting-animations", Label: "animations", Kind: "enum", Default: "auto",
		Desc: "Overlay / toast / list animations: on, off, or auto (off over SSH and when the terminal is not truecolor). The editor text itself never animates."},
	{ID: "setting-images", Label: "images", Kind: "enum", Default: "auto",
		Desc: "Inline images (image preview, dashboard logo, Markdown preview): kitty, sixel, blocks (coloured half-blocks, works everywhere with colour), off, or auto (probe the terminal; blocks inside tmux without allow-passthrough)."},
	{ID: "setting-font-delta", Label: "font_delta", Kind: "int", Default: "-2",
		Desc: "Font size change asked of the host terminal at launch (OSC 1337); 0 turns it off. TERMOCODE_FONT_DELTA wins over it."},
	{ID: "setting-auto-save", Label: "auto_save", Kind: "bool", Default: "false",
		Desc: "Save files automatically after edits."},
	{ID: "setting-word-wrap", Label: "word_wrap", Kind: "bool", Default: "false",
		Desc: "Soft-wrap long lines in the editor."},
	{ID: "setting-show-hidden", Label: "show_hidden", Kind: "bool", Default: "false",
		Desc: "Show dot-files in the explorer."},
	{ID: "setting-tab-size", Label: "tab_size", Kind: "int", Default: "4",
		Desc: "Indent width in spaces (1-16). Sets tabstop and shiftwidth."},
	{ID: "setting-search-max-results", Label: "search_max_results", Kind: "int", Default: strconv.Itoa(search.DefaultMaxResults),
		Desc: "Most results Find in Files shows. 0 means no limit."},
	{ID: "setting-persistent-session", Label: "persistent_session", Kind: "enum", Default: "auto",
		Desc: "Run the editor in a background session that survives a dropped connection; run termocode again in the same folder to get it back: on, off, or auto (on over SSH, off locally and inside tmux / screen). Applies the next time termocode starts; --no-session wins over it."},
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
	if v, ok := aiSettingValue(r.ID); ok { // Group A
		return v
	}
	if v, ok := displaySettingValue(r.ID, cfg); ok {
		return v
	}
	switch r.ID {
	case "setting-theme":
		if cfg.Theme == "" {
			return r.Default // Docs: default lives in settingsRows
		}
		return cfg.Theme
	case "setting-font-delta":
		if cfg.FontDelta == nil {
			return r.Default
		}
		return strconv.Itoa(*cfg.FontDelta)
	case "setting-auto-save":
		if cfg.AutoSave == nil {
			return r.Default
		}
		return strconv.FormatBool(*cfg.AutoSave)
	case "setting-word-wrap":
		if cfg.WordWrap == nil {
			return r.Default
		}
		return strconv.FormatBool(*cfg.WordWrap)
	case "setting-show-hidden":
		if cfg.ShowHidden == nil {
			return r.Default
		}
		return strconv.FormatBool(*cfg.ShowHidden)
	case "setting-tab-size":
		if cfg.TabSize == nil {
			return r.Default
		}
		return strconv.Itoa(*cfg.TabSize)
	case "setting-search-max-results":
		return strconv.Itoa(searchMaxResults(cfg))
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
	if row.Kind == "enum" {
		return m.openSettingChoicePicker(row, current)
	}
	if row.ID == "setting-theme" {
		return m.openThemePicker()
	}
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
	if cmd, ok := m.applyAISetting(row.ID, value); ok { // Group A
		return cmd
	}
	if cmd, ok := m.applyDisplaySetting(row.ID, value); ok {
		return cmd
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
		m.setWrapAll(b)
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
		// Refresh the status bar's indent chip now: changing an option may
		// not redraw anything.
		toastCmd = tea.Batch(toastCmd, m.fetchStateCmd())
	case "setting-search-max-results":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			m.toast, toastCmd = m.toast.Push(toast.Errr, "search_max_results: expected integer ≥ 0 (0 = unlimited)")
			return toastCmd
		}
		cfg.SearchMaxResults = &n
		if err := saveSettings(cfg); err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
			return toastCmd
		}
		// Read fresh on every search — nothing to apply live.
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("search_max_results = %d", n))
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
// before the TUI even starts (cmd/termocode/main.go via ConfigFontDelta), so we don't
// re-apply it here. Best-effort — failures are logged but never block
// startup.
func (m *Model) applyStartupSettings() {
	cfg := loadSettings()
	m.skipShell = keymap.SkipShellSet(cfg.TerminalSkipShell)
	if cfg.AutoSave != nil && *cfg.AutoSave && !m.autoSaveOn {
		_ = m.toggleAutoSave()
	}
	if m.nvim != nil {
		// Called on nvim.ReadyMsg. Applied silently — no "Word wrap
		// enabled" toast on every launch. word_wrap defaults to off (nvim's
		// own default is on, which split long lines mid-word whenever a
		// side panel narrowed the editor). When it is on, wrap at word
		// boundaries and keep the indent of wrapped lines.
		_ = m.nvim.Command("set linebreak breakindent")
		if cfg.WordWrap != nil && *cfg.WordWrap {
			_ = m.nvim.Command("set wrap")
		} else {
			_ = m.nvim.Command("set nowrap")
		}
		// A file shown again in a window gets the window options it had
		// last time (nvim remembers them per buffer), so a word_wrap
		// change did not reach files already open in other tabs. Apply the
		// global value whenever a file enters a window.
		_ = m.nvim.ExecLua(`vim.api.nvim_create_augroup('TermocodeWordWrap', { clear = true })
vim.api.nvim_create_autocmd('BufWinEnter', {
  group = 'TermocodeWordWrap',
  callback = function()
    if vim.bo.buftype == '' then vim.wo.wrap = vim.go.wrap end
  end,
})`)
	}
	if cfg.ShowHidden != nil && *cfg.ShowHidden && !m.explorer.HiddenShown() {
		_ = m.toggleHiddenFiles()
	}
	if cfg.TabSize != nil && *cfg.TabSize > 0 && m.nvim != nil {
		// setglobal: new buffers inherit it, but EditorConfig's buffer-local
		// values still win for the files it covers.
		_ = m.nvim.Command(fmt.Sprintf("setglobal tabstop=%d shiftwidth=%d", *cfg.TabSize, *cfg.TabSize))
	}
}

// setWrapAll turns wrap on or off in every file window and in the global
// value, so files opened later follow it too (a :setlocal only changed the
// current window). Terminal and other special windows keep theirs. Used by
// the word_wrap setting and the Toggle Word Wrap command.
func (m *Model) setWrapAll(on bool) {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local on = ...
		vim.go.wrap = on
		for _, w in ipairs(vim.api.nvim_list_wins()) do
			if vim.bo[vim.api.nvim_win_get_buf(w)].buftype == '' then
				vim.wo[w].wrap = on
			end
		end`, on)
}
