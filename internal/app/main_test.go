package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain points HOME and every XDG dir at a temp folder, so no test in
// this package reads or writes the developer's real ~/.config/termocode
// (errors.log, recents, session …) or clones plugins into ~/.local/share.
// It also cuts the system clipboard and the network off.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "termocode-app-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	home := filepath.Join(dir, "home")
	for _, d := range []string{"config", "data", "state", "cache",
		"plugins/vim-visual-multi/.git", "plugins/nvim-dap/.git",
		".local/share/termocode/plugins/rainbow-delimiters.nvim"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	for k, v := range map[string]string{
		"HOME":                  home,
		"XDG_CONFIG_HOME":       filepath.Join(home, "config"),
		"XDG_DATA_HOME":         filepath.Join(home, "data"),
		"XDG_STATE_HOME":        filepath.Join(home, "state"),
		"XDG_CACHE_HOME":        filepath.Join(home, "cache"),
		"TERMOCODE_PLUGINS_DIR": filepath.Join(home, "plugins"),
		"TERMOCODE_HEADLESS":    "1",
		"DISPLAY":               "",
		"WAYLAND_DISPLAY":       "",
		"HTTP_PROXY":            "http://127.0.0.1:9",
		"HTTPS_PROXY":           "http://127.0.0.1:9",
		"http_proxy":            "http://127.0.0.1:9",
		"https_proxy":           "http://127.0.0.1:9",
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// Guard for TestMain: recordError must land in the temp config dir.
func TestErrorLogIsSandboxed(t *testing.T) {
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("config dir %q is outside the temp dir", dir)
	}
	recordError("sandbox check")
	if _, err := os.Stat(filepath.Join(dir, "errors.log")); err != nil {
		t.Fatalf("errors.log not in sandbox: %v", err)
	}
}
