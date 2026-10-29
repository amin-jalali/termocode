package app

import (
	"os"
	"strings"
	"testing"
)

// G-UPD: update_check is a Settings row (default on) saved in config.json.
func TestUpdateCheckSetting(t *testing.T) {
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	if !updateCheckOn() {
		t.Fatal("default: want on")
	}
	if v, ok := updateSettingValue("setting-update-check"); !ok || v != "true" {
		t.Fatalf("value %q %v", v, ok)
	}
	m := &Model{w: 120, h: 40}
	m.applySetting("setting-update-check", "off")
	if updateCheckOn() {
		t.Fatal("after off: still on")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"update_check": false`) {
		t.Fatalf("config.json:\n%s", b)
	}
	m.applySetting("setting-update-check", "maybe")
	if updateCheckOn() {
		t.Fatal("a bad value changed the setting")
	}
	if c := settingsCategories()["setting-update-check"]; c != "Updates" {
		t.Fatalf("category %q", c)
	}
}
