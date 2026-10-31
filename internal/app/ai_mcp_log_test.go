package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
)

func mcpLogView(m *Model) string {
	return ansi.Strip(strings.Join(renderOutputTab(m, 100, 20), "\n"))
}

// MCPX-30: a server's Show log shows only that server's lines; the
// palette's Show Log shows every server again.
func TestMCPLogPerServer(t *testing.T) {
	m := covMCPModel(t)
	m.output = newOutputStore()
	writeUserMCP(t, `{"mcpServers":{"a":{"command":"a-mcp"},"b":{"command":"b-mcp"}}}`)
	m.aiMCP().Reload()
	m.output.Append(mcpLogChannel, "10:00:00  a: connected: a-srv 1, MCP x", "10:00:01  b: connected: b-srv 1, MCP x")
	m.aiS().mcpSel = "a"
	m.onMCPServerAction("log")
	v := mcpLogView(m)
	if !strings.Contains(v, "a: connected") || strings.Contains(v, "b: connected") || !strings.Contains(v, "only a") {
		t.Errorf("a's log:\n%s", v)
	}
	m.showMCPLog()
	if v := mcpLogView(m); !strings.Contains(v, "b: connected") || strings.Contains(v, "only a") {
		t.Errorf("all servers:\n%s", v)
	}
}

// MCPX-30: mcp_log_level hides the lines below it: a server's debug
// messages show only with debug; errors show at every level.
func TestMCPLogLevel(t *testing.T) {
	m := covMCPModel(t)
	m.output = newOutputStore()
	m.output.Append(mcpLogChannel, "10:00:00  a: debug: cache warm", "10:00:01  a: info: ready",
		"10:00:02  a: warning: slow disk", "10:00:03  a: tools/list failed: boom", "10:00:04  a: error: bad")
	for _, c := range []struct {
		level string
		want  []string
	}{
		{"info", []string{"ready", "slow disk", "boom", "bad"}},
		{"error", []string{"boom", "bad"}},
		{"warning", []string{"slow disk", "boom", "bad"}},
		{"debug", []string{"cache warm", "ready", "slow disk", "boom", "bad"}},
	} {
		level, want := c.level, c.want
		if c, ok := m.applyMCPSetting("setting-mcp-log-level", level); !ok || c == nil {
			t.Fatalf("%s not applied", level)
		}
		m.showMCPLog()
		v := mcpLogView(m)
		for _, w := range want {
			if !strings.Contains(v, w) {
				t.Errorf("%s: lacks %q:\n%s", level, w, v)
			}
		}
		if n := strings.Count(v, "  a: "); n != len(want) {
			t.Errorf("%s: %d lines, want %d:\n%s", level, n, len(want), v)
		}
	}
	if mcp.LoadSettings(mcpConfigPath()).LogLevel != "debug" {
		t.Error("level not saved")
	}
	m.applyMCPSetting("setting-mcp-log-level", "loud")
	if mcp.LoadSettings(mcpConfigPath()).LogLevel != "debug" {
		t.Error("a bad level was saved")
	}
	if v, _ := mcpSettingValue("setting-mcp-log-level"); v != "debug" {
		t.Errorf("row value = %q", v)
	}
}

// MCPX-30: Log on a service row of Itermoc shows only that service.
func TestItermocServiceLogIsPerServer(t *testing.T) {
	m := covMCPModel(t)
	m.output = newOutputStore()
	m.output.Append(mcpLogChannel, "10:00:00  a: started", "10:00:01  b: started")
	m.itermocRun("log", imRow{kind: imRowService, sec: imSecServices, name: "b"})
	if v := mcpLogView(m); strings.Contains(v, "a: started") || !strings.Contains(v, "only b") {
		t.Errorf("b's log:\n%s", v)
	}
}
