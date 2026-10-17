package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Settings modal: the "MCP" section ───────────────────────────────────
//
// The mcp_* keys of config.json (internal/mcp/settings.go). The AI may
// not change them (aiSettingPolicies: never): MCP servers run programs
// and reach outside services.

// mcpSettingKeys maps a Settings row id to its config.json key.
var mcpSettingKeys = map[string]string{
	"setting-mcp-enabled":           "mcp_enabled",
	"setting-mcp-idle-timeout":      "mcp_idle_timeout",
	"setting-mcp-call-timeout":      "mcp_call_timeout",
	"setting-mcp-max-result-kb":     "mcp_max_result_kb",
	"setting-mcp-trust-annotations": "mcp_trust_annotations",
	"setting-mcp-oauth-port":        "mcp_oauth_port",
}

func init() {
	settingsRows = append(settingsRows,
		settingsRow{ID: "setting-mcp-enabled", Label: "mcp_enabled", Kind: "bool", Default: "true",
			Desc: "Offer the tools of the MCP servers in mcp.json to the AI chat. false hides them all without touching mcp.json."},
		settingsRow{ID: "setting-mcp-idle-timeout", Label: "mcp_idle_timeout", Kind: "string", Default: "10m",
			Desc: "Stop an MCP server after this long without a tool call (a local one ends, a remote one drops its session); it starts again on the next call. \"0\" never stops it. A Go duration: 90s, 10m, 1h."},
		settingsRow{ID: "setting-mcp-call-timeout", Label: "mcp_call_timeout", Kind: "string", Default: "60s",
			Desc: "Longest wait for one MCP tool call without an answer or a progress report. A server's \"timeout\" (seconds) in mcp.json wins."},
		settingsRow{ID: "setting-mcp-max-result-kb", Label: "mcp_max_result_kb", Kind: "int", Default: strconv.Itoa(mcp.DefaultMaxResultKB),
			Desc: "Cut an MCP tool result to this many KB before the model sees it."},
		settingsRow{ID: "setting-mcp-trust-annotations", Label: "mcp_trust_annotations", Kind: "bool", Default: "true",
			Desc: "Let MCP tools the server marks read-only run without asking. false: every MCP tool asks, except the ones in the server's \"autoApprove\" list."},
		settingsRow{ID: "setting-mcp-oauth-port", Label: "mcp_oauth_port", Kind: "int", Default: "0",
			Desc: "Port of the sign-in listener on 127.0.0.1 for MCP servers with OAuth. 0 picks a free port each time; set a fixed one for an app that needs a fixed redirect URL (Slack) or for ssh -L port forwarding."},
	)
}

// mcpSettingsCategories puts the MCP rows in their own Settings group.
func mcpSettingsCategories(cats map[string]string) map[string]string {
	for id := range mcpSettingKeys {
		cats[id] = "MCP"
	}
	return cats
}

// mcpDurationText writes a duration short: 0, 90s, 10m, 1h.
func mcpDurationText(d time.Duration) string {
	switch {
	case d == 0:
		return "0"
	case d < 2*time.Minute && d%time.Second == 0:
		return fmt.Sprintf("%ds", d/time.Second)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", d/time.Second)
	}
	return d.String()
}

// mcpSettingValue renders an MCP row's current value. ok=false: not ours.
func mcpSettingValue(id string) (string, bool) {
	if _, ok := mcpSettingKeys[id]; !ok {
		return "", false
	}
	s := mcp.LoadSettings(mcpConfigPath())
	switch id {
	case "setting-mcp-enabled":
		return strconv.FormatBool(s.Enabled), true
	case "setting-mcp-idle-timeout":
		return mcpDurationText(s.IdleTimeout), true
	case "setting-mcp-call-timeout":
		return mcpDurationText(s.CallTimeout), true
	case "setting-mcp-max-result-kb":
		return strconv.Itoa(s.MaxResultKB), true
	case "setting-mcp-trust-annotations":
		return strconv.FormatBool(s.TrustAnnotations), true
	case "setting-mcp-oauth-port":
		return strconv.Itoa(s.OAuthPort), true
	}
	return "", false
}

// applyMCPSetting checks, saves and applies one MCP row. ok=false: not
// ours.
func (m *Model) applyMCPSetting(id, value string) (tea.Cmd, bool) {
	key, ok := mcpSettingKeys[id]
	if !ok {
		return nil, false
	}
	value = strings.TrimSpace(value)
	bad := func(want string) (tea.Cmd, bool) {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Errr, key+": expected "+want)
		return c, true
	}
	var v any
	switch id {
	case "setting-mcp-enabled", "setting-mcp-trust-annotations":
		b, err := parseBoolLoose(value)
		if err != nil {
			return bad("true/false")
		}
		v = b
	case "setting-mcp-idle-timeout", "setting-mcp-call-timeout":
		d, ok := mcp.ParseDuration(value)
		if !ok || (id == "setting-mcp-call-timeout" && d == 0) {
			return bad("a duration like 90s or 10m")
		}
		v = value
	case "setting-mcp-max-result-kb":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 10240 {
			return bad("a number of KB from 1 to 10240")
		}
		v = n
	case "setting-mcp-oauth-port":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return bad("0 (any free port) or a port up to 65535")
		}
		v = n
	}
	if err := mcp.SaveSetting(mcpConfigPath(), key, v); err != nil {
		return m.aiToastErr("Settings error", err), true
	}
	if s := m.aiS(); s.mcp != nil {
		set := mcp.LoadSettings(mcpConfigPath())
		s.mcp.SetTimeouts(set.IdleTimeout, set.CallTimeout)
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, fmt.Sprintf("%s = %v", key, v))
	return c, true
}
