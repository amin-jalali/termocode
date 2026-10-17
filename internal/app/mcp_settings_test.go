package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (plan §5 Settings UI): the Settings modal has an "MCP" section
// with mcp_enabled, the timeouts, the result cap, mcp_trust_annotations
// and mcp_oauth_port. A value is checked, saved in config.json and the
// timeouts apply to a running Manager at once; a bad value is refused
// and nothing is written.
func TestMCPSettingsSection(t *testing.T) {
	m := aiTestModel(t)
	sm := newSettingsModal()
	inMCP := map[string]string{}
	for _, r := range sm.rows {
		if !r.Header && r.Category == "MCP" {
			inMCP[r.Row.Label] = r.Value
		}
	}
	want := map[string]string{"mcp_enabled": "true", "mcp_idle_timeout": "10m", "mcp_call_timeout": "60s",
		"mcp_max_result_kb": "32", "mcp_trust_annotations": "true", "mcp_oauth_port": "0"}
	for k, v := range want {
		if got, ok := inMCP[k]; !ok || got != v {
			t.Errorf("MCP row %s = %q (present %v), want %q", k, got, ok, v)
		}
	}

	mgr := m.aiMCP()
	m.applySetting("setting-mcp-call-timeout", "90s")
	m.applySetting("setting-mcp-idle-timeout", "0")
	m.applySetting("setting-mcp-max-result-kb", "8")
	m.applySetting("setting-mcp-enabled", "false")
	m.applySetting("setting-mcp-trust-annotations", "false")
	m.applySetting("setting-mcp-oauth-port", "8765")
	s := mcp.LoadSettings(mcpConfigPath())
	if s.CallTimeout != 90*time.Second || s.IdleTimeout != 0 || s.MaxResultKB != 8 || s.Enabled ||
		s.TrustAnnotations || s.OAuthPort != 8765 {
		t.Errorf("saved %+v", s)
	}
	if idle, call := mgr.Timeouts(); idle != 0 || call != 90*time.Second {
		t.Errorf("manager timeouts idle=%v call=%v", idle, call)
	}
	if v, _ := aiSettingValue("setting-mcp-call-timeout"); v != "90s" {
		t.Errorf("row value %q", v)
	}

	before, _ := os.ReadFile(mcpConfigPath())
	for id, bad := range map[string]string{"setting-mcp-call-timeout": "soon", "setting-mcp-max-result-kb": "-3",
		"setting-mcp-oauth-port": "70000", "setting-mcp-enabled": "maybe"} {
		m.applySetting(id, bad)
		if after, _ := os.ReadFile(mcpConfigPath()); string(after) != string(before) {
			t.Errorf("%s = %q was saved", id, bad)
		}
	}
	if v := strings.Join(m.toast.View(100, 40), "\n"); !strings.Contains(v, "mcp_") {
		t.Errorf("no error toast for a bad value: %q", v)
	}
}
