package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/toast"
)

// SCN-30: a config.json that is not valid JSON. termocode starts with
// the defaults (ADR 0005), says so once in plain words, never shows the
// first-run card over it and never writes over the user's file.

// writeConfigJSON points XDG_CONFIG_HOME at a temp dir and writes body
// as config.json there.
func writeConfigJSON(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSettingsFileProblemNamesLineAndColumn(t *testing.T) {
	writeConfigJSON(t, "{\n  \"tab_size\": 2,\n  oops\n}\n")
	got := settingsFileProblem()
	if !strings.Contains(got, "not valid JSON") || !strings.Contains(got, "line 3") {
		t.Errorf("problem = %q, want the line of the mistake", got)
	}
}

func TestSettingsFileProblemNamesAWrongType(t *testing.T) {
	writeConfigJSON(t, `{"tab_size": "two"}`)
	if got := settingsFileProblem(); !strings.Contains(got, "tab_size") {
		t.Errorf("problem = %q, want the key with the wrong type", got)
	}
}

func TestSettingsFileProblemIsEmptyForAGoodOrMissingFile(t *testing.T) {
	for name, body := range map[string]string{"good": `{"tab_size": 2}`, "empty": "", "blank": "  \n"} {
		writeConfigJSON(t, body)
		if got := settingsFileProblem(); got != "" {
			t.Errorf("%s file: problem = %q, want none", name, got)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := settingsFileProblem(); got != "" {
		t.Errorf("missing file: problem = %q, want none", got)
	}
}

func TestBrokenConfigToastAtStart(t *testing.T) {
	writeConfigJSON(t, "{oops")
	cmd := configBrokenCmd()
	if cmd == nil {
		t.Fatal("no toast for a broken config.json")
	}
	msg, ok := cmd().(ToastMsg)
	if !ok {
		t.Fatalf("cmd gave %T, want ToastMsg", cmd())
	}
	if msg.Level != toast.Errr || !strings.Contains(msg.Title, "Settings file") {
		t.Errorf("toast = %+v, want an error about the settings file", msg)
	}
	for _, want := range []string{"config.json", "default settings", "not changed", "Open Config: Theme"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("toast body %q does not say %q", msg.Body, want)
		}
	}
}

func TestNoBrokenConfigToastForAGoodFile(t *testing.T) {
	writeConfigJSON(t, `{"setup_done": true}`)
	if cmd := configBrokenCmd(); cmd != nil {
		t.Errorf("toast for a good config.json: %+v", cmd())
	}
}

// The first-run card would offer to save steps into a file termocode
// cannot read; every save would fail. It stays hidden instead.
func TestSetupCardHiddenOverABrokenConfig(t *testing.T) {
	old := setupCardOff
	setupCardOff = false
	t.Cleanup(func() { setupCardOff = old })
	writeConfigJSON(t, "{oops")
	if newSetupState().visible {
		t.Error("setup card shows over a broken config.json")
	}
	writeConfigJSON(t, `{"animations": "off"}`)
	if !newSetupState().visible {
		t.Error("setup card hidden for a fresh, valid config.json")
	}
}

func TestSettingChangeKeepsABrokenConfigAsItIs(t *testing.T) {
	p := writeConfigJSON(t, "{oops")
	c := loadSettings()
	c.TabSize = new(int)
	*c.TabSize = 2
	if err := saveSettings(c); err == nil {
		t.Error("saveSettings over a broken config.json: want an error")
	}
	if b, _ := os.ReadFile(p); string(b) != "{oops" {
		t.Errorf("config.json changed to %q", b)
	}
}
