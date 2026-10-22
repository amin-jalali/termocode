package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (fix 2): the app's own config.json writers never replace a file that
// is not valid JSON, and keep unknown keys.
func TestAppConfigWritersSafe(t *testing.T) {
	writers := map[string]func() error{
		"saveHTTPSetting":        func() error { return saveHTTPSetting("http_timeout", 5) },
		"saveSecretStoreSetting": func() error { return saveSecretStoreSetting("file") },
		"saveSettings":           func() error { return saveSettings(settingsConfig{Theme: "t"}) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			p := filepath.Join(dir, "termocode", "config.json")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			const bad = "{\"unknown_key\": 1, // comment\n"
			if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := write(); err == nil {
				t.Error("no error for a file that is not valid JSON")
			}
			if b, _ := os.ReadFile(p); string(b) != bad {
				t.Errorf("file changed: %q", b)
			}
			good := `{"unknown_key": 1}`
			if err := os.WriteFile(p, []byte(good), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := write(); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"unknown_key": 1`) {
				t.Errorf("unknown key lost: %s", b)
			}
		})
	}
}
