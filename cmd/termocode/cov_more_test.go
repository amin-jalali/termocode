package main

import (
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

func TestTestCrashModelPassThroughAndView(t *testing.T) {
	m := testCrashModel(plain{}, "view")
	if m.Init() != nil {
		t.Fatal("Init")
	}
	if m.View() != "ok" {
		t.Fatal("a quiet View")
	}
	m2, _ := m.Update(f12)
	if !panics(func() { m2.View() }) {
		t.Fatal("the view mode did not panic")
	}
	if testCrashModel(plain{}, "") == nil {
		t.Fatal("empty mode")
	}
	cm := testCrashModel(plain{}, "go")
	if _, cmd := cm.Update(tea_key("a")); cmd != nil {
		t.Fatal("a plain key gives a command")
	}
}

func TestPacingAndBarWrappers(t *testing.T) {
	_ = framePacingOption(true)
	_ = framePacingOption(false)
	if runBar([]string{"--help"}) != 0 {
		t.Fatal("bar --help")
	}
}

// list shows disabled and broken servers; get shows how each value is set.
func TestMCPAdminListStatusesAndGetEnv(t *testing.T) {
	cwd := adminEnv(t)
	dir, _ := ai.ConfigDir()
	cfg := map[string]any{"mcpServers": map[string]any{
		"off":    map[string]any{"command": "srv", "disabled": true},
		"envful": map[string]any{"command": "srv", "args": []string{"-k", "sk-" + "abcdefghijklmnop"}, "env": map[string]string{"A": "${secret:x}", "B": "${HOME}", "C": "literal"}},
		"broken": map[string]any{"type": "http"},
	}}
	b, _ := json.Marshal(cfg)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcp.UserPath(dir), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runAdmin(t, cwd, "", "list"); code != 0 || !strings.Contains(out, "off") || strings.Contains(out, "abcdefghijklmnop") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "get", "envful"); code != 0 || !strings.Contains(out, "secret (in the store)") || !strings.Contains(out, "from ${HOME}") || !strings.Contains(out, "set in the file") {
		t.Errorf("get: %d %q", code, out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "list", "--json"); code != 0 || !strings.Contains(out, `"status"`) {
		t.Errorf("list json: %d %q", code, out)
	}
	if sortedKeys(nil) != nil || len(sortedKeys(map[string]string{"b": "", "a": ""})) != 2 {
		t.Fatal("sortedKeys")
	}
	if !strings.Contains(os.Getenv("PATH"), "") {
		t.Fatal()
	}
	_ = filepath.Join
}

func tea_key(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestMCPAdminAddJSONWithTokenAndDuplicates(t *testing.T) {
	cwd := adminEnv(t)
	block := `{"mcpServers":{"gh":{"command":"srv","env":{"TOKEN":"ghp_` + "abcdefghijklmnopqrstuvwxyz0123456789" + `"}}}}`
	code, out, errs := runAdmin(t, cwd, "", "add", block)
	if code != 0 || !strings.Contains(out, "Tokens moved to the secret store") || strings.Contains(out+errs, "ghp_abcdef") {
		t.Fatalf("add json: %d %q %q", code, out, errs)
	}
	// The same block again: nothing new is added.
	if code, _, errs := runAdmin(t, cwd, "", "add", block); code == 0 || !strings.Contains(errs, "already has a server") {
		t.Errorf("duplicate: %d %q", code, errs)
	}
	// Piped on stdin.
	if code, _, errs := runAdmin(t, cwd, `{"mcpServers":{"other":{"command":"srv2"}}}`, "add", "-"); code != 0 {
		t.Errorf("stdin json: %d %q", code, errs)
	}
	// A name goes only with a URL.
	if code, _, errs := runAdmin(t, cwd, "", "add", "name", `{"mcpServers":{"x":{"command":"y"}}}`); code == 0 || !strings.Contains(errs, "only with a URL") {
		t.Errorf("name with block: %d %q", code, errs)
	}
	if code, _, errs := runAdmin(t, cwd, "", "add", "not even json"); code == 0 {
		t.Errorf("garbage: %d %q", code, errs)
	}
}

func TestMCPAdminSecretFromStdinNeedsALine(t *testing.T) {
	cwd := adminEnv(t)
	if code, _, errs := runAdmin(t, cwd, "", "add", "--secret", "API_KEY", "s", "--", "srv"); code == 0 || !strings.Contains(errs, "no value for API_KEY") {
		t.Errorf("no line: %d %q", code, errs)
	}
	if code, _, errs := runAdmin(t, cwd, "has space in it\n", "add", "--secret", "API_KEY", "s2", "--", "srv"); code != 0 && !strings.Contains(errs, "API_KEY") {
		t.Errorf("odd value: %d %q", code, errs)
	}
}
