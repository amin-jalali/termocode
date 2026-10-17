package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Preset is a ready mcp.json entry for a well-known remote server (the
// servers picker's "Add a server…"). Endpoints are the vendors' public
// ones from docs/plans (checked 2026-10-15); tokens are never literal.
type Preset struct {
	Name    string // the entry name ("linear")
	Title   string // "Linear"
	URL     string
	Headers map[string]string
	OAuth   *OAuthConfig
	// Note says what the user still has to do (sign in, a token, an app).
	Note string
}

// Entry is the preset as an mcpServers entry.
func (p Preset) Entry() map[string]any {
	e := map[string]any{"type": TypeHTTP, "url": p.URL}
	if len(p.Headers) > 0 {
		e["headers"] = p.Headers
	}
	if p.OAuth != nil {
		e["oauth"] = p.OAuth
	}
	return e
}

// Presets lists the remote servers people ask for most.
func Presets() []Preset {
	return []Preset{
		{Name: "linear", Title: "Linear", URL: "https://mcp.linear.app/mcp",
			Note: "Sign in in the browser (OAuth, no app to create)."},
		{Name: "github", Title: "GitHub", URL: "https://api.githubcopilot.com/mcp/",
			Headers: map[string]string{"Authorization": "Bearer ${secret:github_pat}"},
			Note:    "Needs a personal access token (scopes: repo, read:org): Set secret github_pat…. Not for GitHub Enterprise Server."},
		{Name: "gitlab", Title: "GitLab", URL: "https://gitlab.com/api/v4/mcp", OAuth: &OAuthConfig{Scopes: []string{"mcp"}},
			Note: "Sign in in the browser (scope mcp; GitLab Premium / Ultimate with Duo). Self-managed: change the url to your host."},
		{Name: "atlassian", Title: "Atlassian (Jira, Confluence)", URL: "https://mcp.atlassian.com/v2/mcp",
			Note: "Sign in in the browser. If Atlassian refuses this client, use an API token header instead (see the MCP guide)."},
		{Name: "slack", Title: "Slack", URL: "https://mcp.slack.com/mcp",
			OAuth: &OAuthConfig{ClientID: "${secret:slack_client_id}", ClientSecret: "${secret:slack_client_secret}",
				Scopes: []string{"search:read.public", "chat:write"}},
			Note: "Needs your own Slack app: create an internal app at api.slack.com/apps, redirect URL http://127.0.0.1:PORT/callback (set mcp_oauth_port to PORT), then Set secret slack_client_id… and slack_client_secret…, then Sign in."},
		{Name: "notion", Title: "Notion", URL: "https://mcp.notion.com/mcp",
			Note: "Sign in in the browser (OAuth)."},
		{Name: "sentry", Title: "Sentry", URL: "https://mcp.sentry.dev/mcp",
			Note: "Sign in in the browser (OAuth)."},
		{Name: "clickup", Title: "ClickUp", URL: "https://mcp.clickup.com/mcp",
			Note: "Sign in in the browser (OAuth). Only third-party docs exist for this endpoint: if it fails, check ClickUp's MCP page."},
	}
}

// AddServer adds one entry to an mcp.json (created when missing),
// keeping every other entry and key. A name that exists is an error.
func AddServer(path, name string, entry map[string]any) error {
	if !nameRe.MatchString(name) {
		return errors.New("name may only use letters, digits, - and _")
	}
	file := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &file); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	servers, _ := file["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	if _, ok := servers[name]; ok {
		return fmt.Errorf("%s already has a server named %s", path, name)
	}
	servers[name] = entry
	file["mcpServers"] = servers
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
