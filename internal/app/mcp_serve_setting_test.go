//go:build unix

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mcpServeSetMode writes mcp_server into the test config.json.
func mcpServeSetMode(t *testing.T, mode string) string {
	t.Helper()
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcp_server":"`+mode+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mcpServeSocketExists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// Spec (MS-1): mcp_server is auto by default; bad values are auto.
func TestMCPServeMode(t *testing.T) {
	withConfigDir(t)
	if got := mcpServeMode(); got != "auto" {
		t.Fatalf("default = %q", got)
	}
	for in, want := range map[string]string{"on": "on", "off": "off", "auto": "auto", "ON ": "on", "yes": "auto"} {
		mcpServeSetMode(t, in)
		if got := mcpServeMode(); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// Spec (MS-1): off → no listener and no socket file, even when an agent
// tab asks; auto → none at start, one when an agent tab asks; on → one
// at start. Saving config.json applies a change (restart / stop).
func TestMCPServeSettingStartStop(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	defer m.stopMCPServe()

	cfg := mcpServeSetMode(t, "off")
	m.syncMCPServe()
	m.ensureMCPServeForAgent()
	if m.mcpServe != nil || mcpServeSocketExists(path) {
		t.Fatal("off: serving")
	}

	mcpServeSetMode(t, "auto")
	m.onMCPServeConfigSaved(cfg)
	if m.mcpServe != nil || mcpServeSocketExists(path) {
		t.Fatal("auto: serving with no agent tab")
	}
	m.ensureMCPServeForAgent()
	if m.mcpServe == nil || !mcpServeSocketExists(path) {
		t.Fatal("auto: an agent tab did not start it")
	}

	mcpServeSetMode(t, "off")
	m.onMCPServeConfigSaved(filepath.Join(filepath.Dir(cfg), "other.json"))
	if m.mcpServe == nil {
		t.Fatal("saving another file stopped it")
	}
	m.onMCPServeConfigSaved(cfg)
	if m.mcpServe != nil || mcpServeSocketExists(path) {
		t.Fatal("off after save: still serving")
	}

	mcpServeSetMode(t, "on")
	m.onMCPServeConfigSaved(cfg)
	if m.mcpServe == nil || !mcpServeSocketExists(path) {
		t.Fatal("on: not serving")
	}
	first := m.mcpServe
	m.onMCPServeConfigSaved(cfg) // saved again, unchanged
	if m.mcpServe != first {
		t.Fatal("an unchanged save restarted the listener")
	}
	_ = m.Close() // the test harness and the session path close the model
	if m.mcpServe != nil || mcpServeSocketExists(path) {
		t.Fatal("Close left the listener")
	}
}

// Spec (MS-1): mcp_server is a documented file-only setting the AI may
// never change.
func TestMCPServeSettingDocumented(t *testing.T) {
	found := false
	for _, s := range SettingsCatalog() {
		if s.Key == "mcp_server" {
			found = true
			if s.Default != "auto" || s.ID != "" || len(s.Choices) != 3 || s.Description == "" {
				t.Errorf("catalog entry: %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("mcp_server is not in the settings catalog")
	}
	if p, ok := aiSettingPolicies["mcp_server"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("policy: %+v %v", p, ok)
	}
}

// Spec (MCP-6): "Turn on" in the not-listening status saves mcp_server=on
// and starts listening; "Start listening now" (auto) starts for this
// session and leaves the setting alone; a listening status has neither.
func TestMCPServeStatusNotListeningActions(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	defer m.stopMCPServe()

	cfg := mcpServeSetMode(t, "off")
	if id := mcpServeActionID(m.mcpServeStatusItems(), "serve-"); id != "serve-on" {
		t.Fatalf("off: action %q, want serve-on", id)
	}
	m.onMCPServeStatusPicked("serve-on")
	if m.mcpServe == nil || !mcpServeSocketExists(path) {
		t.Fatal("Turn on did not start listening")
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `"mcp_server": "on"`) {
		t.Errorf("config.json: %s", b)
	}
	if id := mcpServeActionID(m.mcpServeStatusItems(), "serve-"); id != "" {
		t.Errorf("a listening status has the action %q", id)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServeStatus {
		t.Error("the status screen did not refresh")
	}

	m.stopMCPServe()
	m.mcpServeForAgent = false
	cfg = mcpServeSetMode(t, "auto")
	if id := mcpServeActionID(m.mcpServeStatusItems(), "serve-"); id != "serve-start" {
		t.Fatalf("auto: action %q, want serve-start", id)
	}
	m.onMCPServeStatusPicked("serve-start")
	if m.mcpServe == nil {
		t.Fatal("Start listening now did not start")
	}
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), `"on"`) {
		t.Errorf("the session start changed the setting: %s", b)
	}
}
