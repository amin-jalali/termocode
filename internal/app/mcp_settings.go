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
	"setting-mcp-github-client-id":  "mcp_github_client_id",
	"setting-mcp-share":             "mcp_share",
	"setting-mcp-log-level":         "mcp_log_level",
	"setting-mcp-tool-search":       "mcp_tool_search",
	"setting-mcp-catalog-url":       "mcp_catalog_url",
	"setting-mcp-auto-update":       "mcp_auto_update",
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
			Desc: "Port of the sign-in listener on 127.0.0.1 for MCP servers with OAuth. 0 takes the first free one of 47391–47395 (the ports termocode's client document lists, so ssh -L 47391:127.0.0.1:47391 works); set another only for an app registered with that port."},
		settingsRow{ID: "setting-mcp-github-client-id", Label: "mcp_github_client_id", Kind: "string", Default: "(built in)",
			Desc: "Client ID of a GitHub OAuth App with Device Flow on, for the GitHub sign-in with a code (Connect a service). Empty uses the one built into termocode (none until it is registered). Not a secret."},
		settingsRow{ID: "setting-mcp-share", Label: "mcp_share", Kind: "bool", Default: "true",
			Desc: "Run each local MCP server once for all your termocode windows with the same server entry and folder, not once per window. It ends when the last window stops using it (mcp_idle_timeout or quit). false: every window runs its own. Remote servers are not affected."},
		settingsRow{ID: "setting-mcp-log-level", Label: "mcp_log_level", Kind: "string", Default: "info",
			Desc: "The lowest level the MCP log (Output: MCP) shows: error, warning, info or debug. Every line is kept, so a lower level shows the older lines too."},
		settingsRow{ID: "setting-mcp-tool-search", Label: "mcp_tool_search", Kind: "string", Default: "auto",
			Desc: "Send the AI only a search tool instead of every MCP tool's full schema; it loads the tools it needs. auto: only when the MCP tools are big (about 10k tokens). on: always. off: never."},
		settingsRow{ID: "setting-mcp-catalog-url", Label: "mcp_catalog_url", Kind: "string", Default: "",
			Desc: "Catalog that AI: MCP: Browse Catalog… reads (an MCP Registry API, for example your team's approved list). Empty: the official MCP Registry. Must be https."},
		settingsRow{ID: "setting-mcp-auto-update", Label: "mcp_auto_update", Kind: "string", Default: "off",
			Desc: "Servers added from the catalog are pinned to a version. off: check only with AI: MCP: Check for Updates. notify: also check once a day when the MCP servers list opens, and tell. on: the same, and update your own pinned servers (the project file is never changed)."},
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
	case "setting-mcp-github-client-id":
		return s.GitHubClientID, true
	case "setting-mcp-share":
		return strconv.FormatBool(s.Share), true
	case "setting-mcp-log-level":
		return s.LogLevel, true
	case "setting-mcp-tool-search":
		return s.ToolSearch, true
	case "setting-mcp-catalog-url":
		return s.CatalogURL, true
	case "setting-mcp-auto-update":
		return s.AutoUpdate, true
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
	case "setting-mcp-enabled", "setting-mcp-trust-annotations", "setting-mcp-share":
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
			return bad("0 (the default ports) or a port up to 65535")
		}
		v = n
	case "setting-mcp-log-level":
		value = strings.ToLower(value)
		if !mcp.ValidLogLevel(value) {
			return bad("error, warning, info or debug")
		}
		v = value
	case "setting-mcp-tool-search":
		value = strings.ToLower(value)
		if value != mcp.ToolSearchAuto && value != mcp.ToolSearchOn && value != mcp.ToolSearchOff {
			return bad("auto, on or off")
		}
		v = value
	case "setting-mcp-auto-update":
		value = strings.ToLower(value)
		if value != mcp.AutoUpdateOff && value != mcp.AutoUpdateNotify && value != mcp.AutoUpdateOn {
			return bad("off, notify or on")
		}
		v = value
	case "setting-mcp-catalog-url":
		if value != "" {
			if err := mcp.CheckCatalogURL(value); err != nil {
				return bad("an https URL, or empty for the official MCP Registry")
			}
			v = value
		}
	case "setting-mcp-github-client-id":
		if value == "(built in)" {
			value = ""
		}
		if value != "" && !mcp.ValidClientID(value) {
			return bad("a client id (letters, digits, . _ -)")
		}
		if value != "" {
			v = value
		}
	}
	if err := mcp.SaveSetting(mcpConfigPath(), key, v); err != nil {
		return m.aiToastErr("Settings error", err), true
	}
	set := mcp.LoadSettings(mcpConfigPath())
	mcp.SetGitHubClientID(set.GitHubClientID)
	if s := m.aiS(); s.mcp != nil {
		s.mcp.SetTimeouts(set.IdleTimeout, set.CallTimeout)
		s.mcp.SetShare(mcpShareOptions(set))
	}
	if id == "setting-mcp-log-level" {
		m.applyMCPLogFilter() // ai_mcp_log.go
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, fmt.Sprintf("%s = %v", key, v))
	return c, true
}
