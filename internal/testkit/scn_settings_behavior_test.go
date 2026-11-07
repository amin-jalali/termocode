package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-30: the settings files go wrong. termocode starts with the
// defaults (ADR 0005), says so once in plain words, and never writes
// over a file the user can still fix. The unwritable config folder is
// SCN-7 (TestSettingWithReadOnlyConfigDir).

func cfgPath(name string) string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", name)
}

// brokenConfig is a hand-edited config.json with a missing comma.
const brokenConfig = "{\n  \"setup_done\": true\n  \"tab_size\": 2\n}\n"

// Spec: Given config.json is not valid JSON,
// when termocode starts, then one error toast says the settings file is
// broken, on which line, that the defaults are used and the file is not
// changed; the first-run card does not show; a setting change says it
// cannot save; and config.json is byte for byte what the user wrote.
func TestStartWithACorruptConfig(t *testing.T) {
	d := New(t, Options{W: 150, H: 50, Config: map[string]string{"config.json": brokenConfig}})
	if !d.WaitForText("Settings file is broken", 3*time.Second) {
		t.Fatalf("no toast about the broken config.json:\n%s", screen(d))
	}
	s := screen(d)
	for _, want := range []string{"line 3", "default settings"} {
		if !strings.Contains(s, want) {
			t.Errorf("the toast does not say %q:\n%s", want, s)
		}
	}
	if cardShown(d) {
		t.Errorf("the first-run card shows over a broken config.json:\n%s", s)
	}
	d.Action("toasts:clear")
	d.Action("setting:setting-tab-size=8")
	if !d.WaitForText("Settings error", 3*time.Second) {
		t.Errorf("a setting change over a broken config.json gave no error:\n%s", screen(d))
	}
	if b, _ := os.ReadFile(cfgPath("config.json")); string(b) != brokenConfig {
		t.Errorf("config.json changed: %q", b)
	}
	d.Restart()
	if !d.WaitForText("Settings file is broken", 3*time.Second) {
		t.Errorf("after a restart the broken file is not named again:\n%s", screen(d))
	}
}

// Spec: Given a valid config.json,
// when termocode starts, then no toast about the settings file shows.
func TestStartWithAGoodConfigHasNoSettingsToast(t *testing.T) {
	d := New(t, Options{W: 150, H: 50})
	d.Settle(300 * time.Millisecond)
	if s := screen(d); strings.Contains(s, "Settings file is broken") {
		t.Errorf("a good config.json is called broken:\n%s", s)
	}
}

// setCustomColor runs Edit Custom Theme, picks key and types hex.
func setCustomColor(d *Driver, key, hex string) {
	d.T.Helper()
	d.Palette("edit-custom-theme")
	if !d.WaitForText("Custom Theme", 2*time.Second) {
		d.T.Fatalf("the custom theme list did not open:\n%s", screen(d))
	}
	d.Type(key)
	d.Key("enter")
	if !d.WaitForText("(#rrggbb)", 2*time.Second) {
		d.T.Fatalf("no colour prompt for %s:\n%s", key, screen(d))
	}
	for range 12 {
		d.Key("backspace")
	}
	d.Type(hex)
	d.Key("enter")
}

// Spec: Given no custom theme yet,
// when the user sets BgEditor in Edit Custom Theme, then the colour is
// saved in user_theme.json, the Custom theme is chosen in config.json,
// a bad hex is refused in plain words, and after a restart the list
// shows the saved colour.
func TestCustomThemeJourney(t *testing.T) {
	d := New(t, Options{W: 150, H: 50})
	setCustomColor(d, "BgEditor", "#123456")
	if !d.WaitForText("#123456", 2*time.Second) {
		t.Fatalf("no toast with the new colour:\n%s", screen(d))
	}
	var saved map[string]string
	b, _ := os.ReadFile(cfgPath("user_theme.json"))
	if err := json.Unmarshal(b, &saved); err != nil || saved["BgEditor"] != "#123456" {
		t.Errorf("user_theme.json = %q (%v)", b, err)
	}
	var cfg map[string]any
	b, _ = os.ReadFile(cfgPath("config.json"))
	if err := json.Unmarshal(b, &cfg); err != nil || cfg["theme"] != "custom" {
		t.Errorf("config.json theme = %v (%v), want custom", cfg["theme"], err)
	}
	if cfg["setup_done"] != true {
		t.Errorf("config.json lost setup_done: %s", b)
	}

	d.Action("toasts:clear")
	setCustomColor(d, "TextPrimary", "blue")
	if !d.WaitForText("expected #rgb or #rrggbb", 2*time.Second) {
		t.Errorf("a bad colour was not refused:\n%s", screen(d))
	}
	d.Key("esc")

	d.Restart()
	d.Palette("edit-custom-theme")
	if !d.WaitForText("#123456", 2*time.Second) {
		t.Errorf("after a restart the list does not show the saved colour:\n%s", screen(d))
	}
}

// Spec: Given a hand-edited user_theme.json that is not valid JSON,
// when the user sets a colour, then the new colour is saved, the old
// file is kept as user_theme.json.broken, and a toast names it.
func TestCustomThemeOverACorruptFile(t *testing.T) {
	const broken = `{"BgEditor": "#101010",,}`
	d := New(t, Options{W: 150, H: 50, Config: map[string]string{"user_theme.json": broken}})
	setCustomColor(d, "AccentBlue", "#0000ff")
	if !d.WaitForText("user_theme.json.broken", 2*time.Second) {
		t.Errorf("no toast naming the kept file:\n%s", screen(d))
	}
	if b, err := os.ReadFile(cfgPath("user_theme.json.broken")); err != nil || string(b) != broken {
		t.Errorf("old file not kept: %q %v", b, err)
	}
	var saved map[string]string
	b, _ := os.ReadFile(cfgPath("user_theme.json"))
	if err := json.Unmarshal(b, &saved); err != nil || saved["AccentBlue"] != "#0000ff" {
		t.Errorf("user_theme.json = %q (%v)", b, err)
	}
}
