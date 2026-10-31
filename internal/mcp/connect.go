package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/auth"
)

// The "Connect a service" planner (CX-4): which ways to connect a
// service has on this machine, best first. The wizard in
// internal/app/ai_mcp_connect.go shows the first one and offers the
// next ones as fallbacks. Token pasting is always last.
//
//	cli (gh) → device (GitHub; GitLab with an app id) → browser (laptop)
//	         → paste (SSH, no device flow) → token

// Method is one way to connect.
type Method string

const (
	MethodCLI     Method = "cli"     // a CLI's login (gh auth token)
	MethodDevice  Method = "device"  // a code typed on the service's page
	MethodBrowser Method = "browser" // OAuth in this machine's browser (loopback)
	MethodPaste   Method = "paste"   // OAuth over SSH: paste the address back
	MethodToken   Method = "token"   // a personal token (the last way)
	MethodApp     Method = "app"     // create your own app first (Slack)
)

// PlanEnv is what the planner knows about this machine.
type PlanEnv struct {
	GHAccounts     []GHAccount // gh is signed in with these (nil: no gh login)
	SSH            bool        // the browser is on another machine
	GitHubClientID string      // termocode's GitHub OAuth App ("" not registered yet)
	AppClientID    string      // the user's own app id (Slack, GitLab device)
}

// Plan lists the ways to connect p, best first.
func Plan(p Preset, env PlanEnv) []Method {
	var out []Method
	if p.CLI == "gh" && len(env.GHAccounts) > 0 {
		out = append(out, MethodCLI)
	}
	if p.App != nil && env.AppClientID == "" {
		return []Method{MethodApp}
	}
	device := (p.Device != nil && env.GitHubClientID != "") || (p.DeviceWithApp && env.AppClientID != "")
	switch {
	case p.NoBrowser:
		if device {
			out = append(out, MethodDevice)
		}
	case env.SSH:
		if device {
			out = append(out, MethodDevice)
		}
		out = append(out, MethodPaste)
	default:
		out = append(out, MethodBrowser)
		if device {
			out = append(out, MethodDevice)
		}
	}
	if p.Token != nil || (p.Name != "" && !isPreset(p.Name)) {
		out = append(out, MethodToken)
	}
	return out
}

func isPreset(name string) bool {
	for _, p := range basePresets() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// CleanAccount makes an account label safe to show and to put into the
// system prompt: [A-Za-z0-9 ._@-] only, at most 64 characters.
func CleanAccount(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._@-", r)) {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// IdentityResult is what the test call after a sign-in found.
type IdentityResult struct {
	Account string // "amin-jalali" ("" unknown)
	Server  string // initialize serverInfo name
	Tools   int
}

// Identity connects a server, lists its tools and calls its identity
// tool when it has one. A failing identity call is not an error (the
// account stays unknown); a failing connect is.
func (m *Manager) Identity(ctx context.Context, name, tool string) (IdentityResult, error) {
	st, err := m.get(name)
	if err != nil {
		return IdentityResult{}, err
	}
	tools, err := m.list(ctx, st, nil)
	if err != nil {
		return IdentityResult{}, err
	}
	res := IdentityResult{Tools: len(tools)}
	m.mu.Lock()
	if st.proc != nil {
		res.Server = CleanAccount(st.proc.Client.ServerInfo().Name)
	}
	m.mu.Unlock()
	if tool == "" {
		return res, nil
	}
	found := false
	for _, t := range tools {
		if t.Name == tool {
			found = true
		}
	}
	if !found {
		return res, nil
	}
	r, err := m.Call(ctx, nil, name, tool, map[string]any{})
	if err != nil || r.IsError {
		if err != nil {
			m.log(name, "identity call %s failed: %s", tool, ai.Redact(err.Error()))
		}
		return res, nil
	}
	res.Account = accountFrom(r)
	return res, nil
}

// accountFrom finds a login / user name in an identity tool's answer.
func accountFrom(r CallResult) string {
	var v map[string]any
	if len(r.StructuredContent) > 0 {
		_ = json.Unmarshal(r.StructuredContent, &v)
	}
	if v == nil {
		_ = json.Unmarshal([]byte(strings.TrimSpace(r.Text())), &v)
	}
	if v != nil {
		if u, ok := v["user"].(map[string]any); ok {
			v = u
		}
		for _, k := range []string{"login", "username", "email", "name"} {
			if s, ok := v[k].(string); ok && CleanAccount(s) != "" {
				return CleanAccount(s)
			}
		}
		return ""
	}
	// A text answer ("You are amin@example.com …"): the first word with @.
	for _, f := range strings.Fields(r.Text()) {
		if strings.Contains(f, "@") {
			return CleanAccount(strings.Trim(f, ".,;:()"))
		}
	}
	return ""
}

// SetAccount stores the account label of a server: next to its OAuth
// sign-in when it has one, else in mcp_accounts. "" forgets it.
func (c CredStore) SetAccount(name, account string) error {
	account = CleanAccount(account)
	return c.update(func(cr *ai.Credentials) {
		if e, ok := cr.MCPOAuth[name]; ok {
			e.Account = account
			cr.MCPOAuth[name] = e
		}
		if account == "" {
			delete(cr.MCPAccounts, name)
			return
		}
		if _, ok := cr.MCPOAuth[name]; ok {
			delete(cr.MCPAccounts, name)
			return
		}
		if cr.MCPAccounts == nil {
			cr.MCPAccounts = map[string]string{}
		}
		cr.MCPAccounts[name] = account
	})
}

// Account is the stored account label of a server ("" unknown).
func (c CredStore) Account(name string) string {
	credMu.Lock()
	defer credMu.Unlock()
	cr, _ := ai.LoadCredentials(c.Path)
	if e, ok := cr.MCPOAuth[name]; ok && e.Account != "" {
		return e.Account
	}
	return cr.MCPAccounts[name]
}

// DeviceSignIn starts the device flow of a server: with a shipped app
// (app.DeviceEndpoint set: GitHub) or, for a user's own app id, at the
// endpoints discovery finds (GitLab). The caller shows UserCode and
// VerificationURI, then Polls and calls FinishSignIn.
func (m *Manager) DeviceSignIn(ctx context.Context, name string, app DeviceApp, clientID string) (*auth.DeviceFlow, error) {
	cfg := auth.DeviceConfig{DeviceEndpoint: app.DeviceEndpoint, TokenEndpoint: app.TokenEndpoint, Issuer: app.Issuer,
		ClientID: clientID, Scopes: app.Scopes, Resource: app.Resource, NoResourceParam: app.NoResourceParam, HTTP: m.opts.HTTP}
	if cfg.DeviceEndpoint == "" {
		st, err := m.get(name)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		u, meta := st.cfg.URL, st.authMeta
		override := ""
		if st.cfg.OAuth != nil {
			override = st.cfg.OAuth.MetadataURL
		}
		m.mu.Unlock()
		md, err := auth.DiscoverWith(ctx, m.opts.HTTP, u, meta, override)
		if err != nil {
			return nil, err
		}
		if md.DeviceEndpoint == "" {
			return nil, errDeviceUnsupported
		}
		cfg.DeviceEndpoint, cfg.TokenEndpoint, cfg.Issuer, cfg.Resource = md.DeviceEndpoint, md.TokenEndpoint, md.Issuer, md.Resource
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = md.Scopes
		}
	}
	m.log(name, "device sign-in started")
	d, err := auth.BeginDevice(ctx, cfg)
	if err != nil {
		m.log(name, "device sign-in failed: %s", ai.Redact(err.Error()))
	}
	return d, err
}

var errDeviceUnsupported = deviceUnsupported{}

type deviceUnsupported struct{}

func (deviceUnsupported) Error() string {
	return "this service does not offer the code sign-in (device flow)"
}
