package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// A settings change that cannot be saved says so and is not applied.
func TestCovCoreSettingSaveFails(t *testing.T) {
	root := withConfigDir(t)
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	m := New()
	m.w, m.h = 100, 30
	// A file where the termocode config folder must go.
	_ = os.RemoveAll(filepath.Join(root, "termocode"))
	if err := os.WriteFile(filepath.Join(root, "termocode"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for id, value := range map[string]string{
		"setting-theme":              "tokyo-night",
		"setting-font-delta":         "-1",
		"setting-auto-save":          "true",
		"setting-word-wrap":          "on",
		"setting-show-hidden":        "yes",
		"setting-auto-reconnect":     "off",
		"setting-key-hints":          "0",
		"setting-tab-size":           "2",
		"setting-search-max-results": "500",
		"setting-persistent-session": "on",
	} {
		before := strings.Count(toastText(m), "Settings error")
		_ = m.applySetting(id, value)
		if strings.Count(toastText(m), "Settings error") <= before && !strings.Contains(toastText(m), "error") {
			t.Errorf("%s=%s on a blocked config: no error toast\n%s", id, value, toastText(m))
		}
		m.toast = toast.New()
	}
	// Unknown rows do nothing; a missing config file reads as empty.
	if m.applySetting("setting-nope", "1") != nil || m.onSettingsRowSelected("setting-nope") != nil {
		t.Error("an unknown row did something")
	}
	if got := loadSettings(); got.Theme != "" {
		t.Errorf("blocked config read as %+v", got)
	}
}

// No XDG_CONFIG_HOME: config.json lives under HOME; no HOME: no path.
func TestCovCoreSettingsPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if p, err := settingsConfigPath(); err != nil || p != filepath.Join(home, ".config", "termocode", "config.json") {
		t.Errorf("path = %q, %v", p, err)
	}
	t.Setenv("HOME", "")
	if _, err := settingsConfigPath(); err == nil {
		t.Error("no HOME: no error")
	}
	if got := loadSettings(); got.Theme != "" || got.AutoSave != nil {
		t.Errorf("no path: %+v", got)
	}
	if err := saveSettings(settingsConfig{Theme: "x"}); err == nil {
		t.Error("saved without a path")
	}
}

// Bad values are refused with a short reason.
func TestCovCoreSettingBadValues(t *testing.T) {
	withConfigDir(t)
	m := New()
	m.w, m.h = 100, 30
	for id, want := range map[string]string{
		"setting-font-delta":         "font_delta: not an integer",
		"setting-auto-save":          "auto_save: expected true/false",
		"setting-word-wrap":          "word_wrap: expected true/false",
		"setting-show-hidden":        "show_hidden: expected true/false",
		"setting-auto-reconnect":     "auto_reconnect",
		"setting-key-hints":          "key_hints",
		"setting-tab-size":           "tab_size",
		"setting-search-max-results": "search_max_results",
	} {
		_ = m.applySetting(id, "maybe")
		if !strings.Contains(toastText(m), want) {
			t.Errorf("%s=maybe: toast misses %q\n%s", id, want, toastText(m))
		}
		m.toast = toast.New()
	}
	// Int and bool rows open a prompt with a hint.
	_ = m.onSettingsRowSelected("setting-tab-size")
	if !m.promptOpen || m.promptKind != promptKindSetting {
		t.Error("tab size prompt")
	}
	_ = m.onSettingsRowSelected("setting-theme")
	if !m.pickerOpen || m.pickerKind != pickerKindTheme {
		t.Error("theme row opens the theme picker")
	}
}
