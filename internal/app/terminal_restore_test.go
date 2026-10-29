package app

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestConfig writes config.json in the test's config folder.
func writeTestConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termocode", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalRestoreSettings(t *testing.T) {
	dir := withConfigDir(t)
	if !terminalRestoreOn() || terminalRestoreCommandsOn() {
		t.Fatal("defaults: restore on, commands off")
	}
	writeTestConfig(t, dir, `{"terminal_restore": false, "terminal_restore_commands": true}`)
	if terminalRestoreOn() || !terminalRestoreCommandsOn() {
		t.Fatal("config.json values are not read")
	}
	writeTestConfig(t, dir, `{"terminal_restore": "yes"`)
	if !terminalRestoreOn() || terminalRestoreCommandsOn() {
		t.Fatal("a broken config.json keeps the defaults")
	}
}
