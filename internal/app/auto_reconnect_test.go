package app

import (
	"strings"
	"testing"
)

// TestAutoReconnectSetting: auto_reconnect is on unless the user turns it
// off in Settings; the value is saved for the next start.
func TestAutoReconnectSetting(t *testing.T) {
	withConfigDir(t)
	if !ConfigAutoReconnect() {
		t.Fatal("auto_reconnect is off by default")
	}
	row, ok := findSettingsRow("setting-auto-reconnect")
	if !ok {
		t.Fatal("no auto_reconnect row in Settings")
	}
	if got := currentSettingValue(row, loadSettings()); got != "true" {
		t.Errorf("shown as %q, want true", got)
	}
	m := New()
	m.applySetting(row.ID, "false")
	if ConfigAutoReconnect() {
		t.Error("still on after turning it off")
	}
	if got := toastText(m); !strings.Contains(got, "next time termocode starts") {
		t.Errorf("toast %q does not say when it applies", got)
	}
	m.applySetting(row.ID, "maybe")
	if ConfigAutoReconnect() {
		t.Error("a bad value turned it on")
	}
}
