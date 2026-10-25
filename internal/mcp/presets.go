package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// GitHubClientID is termocode's public GitHub OAuth App client ID for
// device flow. Builds without a registered app leave it empty; release
// builds may set it at build time:
//
//	go build -ldflags "-X github.com/amin-jalali/termocode/internal/mcp.GitHubClientID=Ov23li…"
//
// The mcp_github_client_id setting overrides it. While it is empty the
// wizard skips the GitHub device step (gh and a token still work).
var GitHubClientID = ""

// Preset is a well-known remote server for the "Connect a service"
// wizard. Endpoints are the services' public endpoints; tokens are never
// stored in this list.
type Preset struct {
	Name  string // the entry name ("linear")
	Title string // "Linear"
	Blurb string // what it gives the AI: "issues, projects"
	// URL is the full (read and write) endpoint; ReadOnlyURL the
	// read-only one ("" when the service has none: the scopes limit it).
	URL, ReadOnlyURL string
	// ReadScopes / WriteScopes are the OAuth scopes asked for (nil: the
	// server's own list).
	ReadScopes, WriteScopes []string
	// Identity is the tool that names the signed-in account ("get_me").
	Identity string
	// Try is a first thing to ask the AI.
	Try string
	// Token is the "use a token instead" way (nil: there is none).
	Token *TokenRecipe
	// Device is a shipped device-flow app (GitHub); DeviceWithApp: the
	// device flow works with a user's own app id (GitLab).
	Device        *DeviceApp
	DeviceWithApp bool
	// CLI is a command-line tool whose login can be used ("gh").
	CLI string
	// NoBrowser: no browser sign-in for an unknown client (GitHub's web
	// flow needs a client secret, which an open-source binary can not
	// keep).
	NoBrowser bool
	// App is the "create your own app" recipe (Slack): the browser
	// sign-in needs a client id the user pastes.
	App *AppRecipe
	// Note is a short line about limits (shown on the error screens).
	Note string
}

// TokenRecipe is how to make and send a personal token.
type TokenRecipe struct {
	Page   string   // where the token is made
	Scopes []string // the smallest scopes to tick
	Scheme string   // Authorization scheme: "Bearer", "Sentry-Bearer", "Basic"
	// Email: the token goes with the account's e-mail (Atlassian Basic).
	Email bool
}

// SecretName is the mcp_secrets key of a preset's token.
func (p Preset) SecretName() string { return p.Name + "_token" }

// DeviceApp is a registered public client with the device flow.
type DeviceApp struct {
	ClientID       func() string // read at use (build-time value or setting)
	DeviceEndpoint string
	TokenEndpoint  string
	Issuer         string
	Scopes         []string
	// Resource binds the tokens to the MCP server; NoResourceParam: the
	// token endpoint does not take RFC 8707 "resource" (github.com).
	Resource        string
	NoResourceParam bool
}

// AppRecipe is the one-time "create your own app" setup (Slack).
type AppRecipe struct {
	Page         string   // where the app is created
	Steps        []string // what to click, in order
	Port         int      // the fixed loopback port of the redirect
	RedirectHost string   // "localhost"
	UserScopes   []string // read set first
}

// Manifest is the app manifest the user pastes (JSON; no secrets).
func (r AppRecipe) Manifest() string {
	m := map[string]any{
		"display_information": map[string]any{"name": "termocode", "description": "termocode's AI chat reads Slack through the Slack MCP server."},
		"oauth_config": map[string]any{
			"redirect_urls": []string{fmt.Sprintf("http://%s:%d/callback", r.RedirectHost, r.Port)},
			"scopes":        map[string]any{"user": r.UserScopes},
		},
		"settings": map[string]any{"org_deploy_enabled": false, "socket_mode_enabled": false, "token_rotation_enabled": false},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// RedirectURI is the recipe's redirect URL.
func (r AppRecipe) RedirectURI() string {
	return fmt.Sprintf("http://%s:%d/callback", r.RedirectHost, r.Port)
}

// githubClientIDOverride is mcp_github_client_id (set by the app).
var (
	presetMu               sync.Mutex
	githubClientIDOverride string
	presetOverride         func([]Preset) []Preset
)

// SetGitHubClientID sets the mcp_github_client_id setting ("" = the
// build-time GitHubClientID).
func SetGitHubClientID(id string) {
	presetMu.Lock()
	githubClientIDOverride = strings.TrimSpace(id)
	presetMu.Unlock()
}

// GitHubDeviceClientID is the GitHub OAuth App id in use ("" none).
func GitHubDeviceClientID() string {
	presetMu.Lock()
	defer presetMu.Unlock()
	if githubClientIDOverride != "" {
		return githubClientIDOverride
	}
	return GitHubClientID
}

// OverridePresets changes the preset list (tests: point the endpoints at
// fakes on 127.0.0.1). It returns the function that restores the list.
func OverridePresets(fn func([]Preset) []Preset) (restore func()) {
	presetMu.Lock()
	presetOverride = fn
	presetMu.Unlock()
	return func() {
		presetMu.Lock()
		presetOverride = nil
		presetMu.Unlock()
	}
}

// Presets lists the services of the Connect wizard, in its order.
func Presets() []Preset {
	ps := basePresets()
	presetMu.Lock()
	fn := presetOverride
	presetMu.Unlock()
	if fn != nil {
		ps = fn(ps)
	}
	return ps
}

// PresetByName finds a preset.
func PresetByName(name string) (Preset, bool) {
	for _, p := range Presets() {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

func basePresets() []Preset {
	return []Preset{
		{Name: "github", Title: "GitHub", Blurb: "issues, pull requests, code",
			URL: "https://api.githubcopilot.com/mcp/", ReadOnlyURL: "https://api.githubcopilot.com/mcp/readonly",
			Identity: "get_me", Try: "show my open pull requests", CLI: "gh", NoBrowser: true,
			Device: &DeviceApp{ClientID: GitHubDeviceClientID,
				DeviceEndpoint: "https://github.com/login/device/code", TokenEndpoint: "https://github.com/login/oauth/access_token",
				Issuer: "https://github.com/login/oauth", Scopes: []string{"read:user", "read:org", "repo"},
				Resource: "https://api.githubcopilot.com/mcp/", NoResourceParam: true},
			Token: &TokenRecipe{Page: "https://github.com/settings/personal-access-tokens/new", Scopes: []string{"repo", "read:org"}, Scheme: "Bearer"},
			Note:  "GitHub Enterprise Server has no remote MCP server."},
		{Name: "gitlab", Title: "GitLab", Blurb: "issues, merge requests, CI",
			URL: "https://gitlab.com/api/v4/mcp", ReadScopes: []string{"mcp"}, WriteScopes: []string{"mcp"},
			Try: "list my open merge requests", DeviceWithApp: true,
			Token: &TokenRecipe{Page: "https://gitlab.com/-/user_settings/personal_access_tokens", Scopes: []string{"MCP tool: Execute (fine-grained)"}, Scheme: "Bearer"},
			Note:  "Self-managed GitLab: change the url to your host."},
		{Name: "linear", Title: "Linear", Blurb: "issues, projects",
			URL: "https://mcp.linear.app/mcp", ReadOnlyURL: "https://mcp.linear.app/mcp/readonly",
			ReadScopes: []string{"read"}, WriteScopes: []string{"read", "write"}, Try: "what issues are assigned to me?",
			Token: &TokenRecipe{Page: "https://linear.app/settings/account/security", Scopes: []string{"Read"}, Scheme: "Bearer"}},
		{Name: "notion", Title: "Notion", Blurb: "pages, databases",
			URL: "https://mcp.notion.com/mcp", Try: "find my notes about the roadmap",
			Token: &TokenRecipe{Page: "https://www.notion.so/profile/integrations", Scopes: []string{"MCP token (Business / Enterprise)"}, Scheme: "Bearer"}},
		{Name: "sentry", Title: "Sentry", Blurb: "errors, issues",
			URL: "https://mcp.sentry.dev/mcp", ReadScopes: []string{"org:read"}, Identity: "whoami", Try: "what are the newest unresolved errors?",
			Token: &TokenRecipe{Page: "https://sentry.io/settings/account/api/auth-tokens/", Scopes: []string{"org:read", "project:read", "event:read"}, Scheme: "Sentry-Bearer"}},
		{Name: "clickup", Title: "ClickUp", Blurb: "tasks, docs, chat",
			URL: "https://mcp.clickup.com/mcp", ReadScopes: []string{"read"}, WriteScopes: []string{"read", "write"}, Try: "what tasks are due this week?",
			Note: "ClickUp has no refresh: sign in again when the sign-in ends."},
		{Name: "atlassian", Title: "Atlassian", Blurb: "Jira, Confluence",
			URL: "https://mcp.atlassian.com/v2/mcp", Try: "what Jira issues are assigned to me?",
			Token: &TokenRecipe{Page: "https://id.atlassian.com/manage-profile/security/api-tokens", Scopes: []string{"API token (only if your admin allows it)"}, Scheme: "Basic", Email: true},
			Note:  "Needs a Jira or Confluence Cloud site; tool calls use Rovo credits."},
		{Name: "slack", Title: "Slack", Blurb: "search, read channels",
			URL: "https://mcp.slack.com/mcp", Try: "what did the team say about the release?",
			App: &AppRecipe{Page: "https://api.slack.com/apps?new_app=1", Port: 47391, RedirectHost: "localhost",
				UserScopes: []string{"search:read.public", "channels:history", "users:read"},
				Steps: []string{
					"api.slack.com/apps → Create New App → From a manifest.",
					"Paste the manifest (it is copied) → Create.",
					"Turn on PKCE (OAuth & Permissions) and the MCP server (Agents & AI Apps).",
					"Copy the Client ID from Basic Information (not the secret).",
				}},
			Token: &TokenRecipe{Page: "https://api.slack.com/apps", Scopes: []string{"a user token (xoxp-…) from your app"}, Scheme: "Bearer"},
			Note:  "Slack lets only registered apps in; a workspace admin may need to approve yours."},
	}
}

// EntryOptions shape the mcp.json entry of a connection.
type EntryOptions struct {
	Writes      bool   // the full endpoint and the write scopes
	GHUser      string // MethodCLI: the gh account
	AppClientID string // the user's own app id (Slack, GitLab device)
	Secret      string // MethodToken: the mcp_secrets name ("" = <preset>_token)
}

// EntryFor is the mcpServers entry the wizard writes for a method.
// Secrets are always ${secret:NAME}; nothing here is a secret.
func (p Preset) EntryFor(m Method, o EntryOptions) map[string]any {
	u := p.URL
	if !o.Writes && p.ReadOnlyURL != "" {
		u = p.ReadOnlyURL
	}
	e := map[string]any{"type": TypeHTTP, "url": u}
	switch m {
	case MethodCLI:
		e["auth"] = &AuthConfig{GH: o.GHUser}
	case MethodToken:
		scheme := "Bearer"
		if p.Token != nil && p.Token.Scheme != "" {
			scheme = p.Token.Scheme
		}
		secret := o.Secret
		if secret == "" {
			secret = p.SecretName()
		}
		e["headers"] = map[string]string{"Authorization": scheme + " ${secret:" + secret + "}"}
	default:
		scopes := p.ReadScopes
		if o.Writes && p.WriteScopes != nil {
			scopes = p.WriteScopes
		}
		if len(scopes) > 0 || o.AppClientID != "" {
			e["oauth"] = &OAuthConfig{ClientID: o.AppClientID, Scopes: scopes}
		}
	}
	return e
}

// Entry is the preset's default entry (the browser sign-in, read-only).
func (p Preset) Entry() map[string]any { return p.EntryFor(MethodBrowser, EntryOptions{}) }

// AddServer adds one entry to an mcp.json (created when missing),
// keeping every other entry and key. A name that exists is an error.
func AddServer(path, name string, entry map[string]any) error {
	return editServers(path, func(servers map[string]any) error {
		if _, ok := servers[name]; ok {
			return fmt.Errorf("%s already has a server named %s", path, name)
		}
		if !nameRe.MatchString(name) {
			return errors.New("name may only use letters, digits, - and _")
		}
		servers[name] = entry
		return nil
	})
}

// PutServer adds or replaces one entry (the wizard connecting again).
func PutServer(path, name string, entry map[string]any) error {
	return editServers(path, func(servers map[string]any) error {
		if !nameRe.MatchString(name) {
			return errors.New("name may only use letters, digits, - and _")
		}
		servers[name] = entry
		return nil
	})
}

// SetServerKey changes one key of an existing entry (nil removes it):
// Allow writes ("url"), Switch account ("disabled").
func SetServerKey(path, name, key string, value any) error {
	return editServers(path, func(servers map[string]any) error {
		e, ok := servers[name].(map[string]any)
		if !ok {
			return fmt.Errorf("%s has no server named %s", path, name)
		}
		if value == nil {
			delete(e, key)
		} else {
			e[key] = value
		}
		return nil
	})
}

// SetDisabled turns one entry off ("disabled": true) or on (the key goes).
// Every other key stays.
func SetDisabled(path, name string, off bool) error {
	if off {
		return SetServerKey(path, name, "disabled", true)
	}
	return SetServerKey(path, name, "disabled", nil)
}

// RemoveServer deletes one entry (no error when it is not there).
func RemoveServer(path, name string) error {
	return editServers(path, func(servers map[string]any) error {
		delete(servers, name)
		return nil
	})
}

func editServers(path string, fn func(map[string]any) error) error {
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
	if err := fn(servers); err != nil {
		return err
	}
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
