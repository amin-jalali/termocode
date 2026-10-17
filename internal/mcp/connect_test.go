package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func mcpOAuthFor(resource string) ai.MCPOAuth {
	return ai.MCPOAuth{Resource: resource, AccessToken: "at-test-123456789"}
}

func preset(t *testing.T, name string) Preset {
	t.Helper()
	p, ok := PresetByName(name)
	if !ok {
		t.Fatalf("no preset %s", name)
	}
	return p
}

func methods(ms []Method) string {
	var out []string
	for _, m := range ms {
		out = append(out, string(m))
	}
	return strings.Join(out, ",")
}

// Spec (CX-4): the fallback ladder per service and machine:
// cli (gh) → device → browser (laptop) → paste (SSH) → token (last).
func TestPlan(t *testing.T) {
	const id = "Ov23liTEST"
	gh := []GHAccount{{Login: "amin-jalali", Active: true}}
	cases := []struct {
		name string
		env  PlanEnv
		want string
	}{
		{"github", PlanEnv{GHAccounts: gh, GitHubClientID: id}, "cli,device,token"},
		{"github", PlanEnv{SSH: true, GitHubClientID: id}, "device,token"},
		{"github", PlanEnv{}, "token"}, // no shipped client id yet: the device step is skipped
		{"github", PlanEnv{GHAccounts: gh}, "cli,token"},
		{"linear", PlanEnv{}, "browser,token"},
		{"linear", PlanEnv{SSH: true}, "paste,token"},
		{"clickup", PlanEnv{}, "browser"},
		{"clickup", PlanEnv{SSH: true}, "paste"},
		{"slack", PlanEnv{}, "app"},
		{"slack", PlanEnv{AppClientID: "1234.5678"}, "browser,token"},
		{"gitlab", PlanEnv{SSH: true, AppClientID: "glapp"}, "device,paste,token"},
		{"gitlab", PlanEnv{AppClientID: "glapp"}, "browser,device,token"},
		{"gitlab", PlanEnv{SSH: true}, "paste,token"},
		{"notion", PlanEnv{}, "browser,token"},
		{"sentry", PlanEnv{SSH: true}, "paste,token"},
		{"atlassian", PlanEnv{}, "browser,token"},
	}
	for _, c := range cases {
		if got := methods(Plan(preset(t, c.name), c.env)); got != c.want {
			t.Errorf("Plan(%s, %+v) = %s, want %s", c.name, c.env, got, c.want)
		}
	}
	custom := Preset{Name: "custom", Title: "Custom", URL: "https://x.example/mcp"}
	if got := methods(Plan(custom, PlanEnv{SSH: true})); got != "paste,token" {
		t.Errorf("custom over SSH: %s", got)
	}
}

// Spec (§6): read-only first. The entry the wizard writes uses the
// read-only endpoint / scopes; Allow writes switches to the full one.
func TestPresetEntries(t *testing.T) {
	gh := preset(t, "github")
	e := gh.EntryFor(MethodCLI, EntryOptions{GHUser: "amin-jalali"})
	if e["url"] != "https://api.githubcopilot.com/mcp/readonly" || e["auth"].(*AuthConfig).GH != "amin-jalali" || e["headers"] != nil {
		t.Errorf("gh entry %v", e)
	}
	if e := gh.EntryFor(MethodCLI, EntryOptions{GHUser: "x", Writes: true}); e["url"] != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("gh writes %v", e)
	}
	if e := gh.EntryFor(MethodToken, EntryOptions{}); e["headers"].(map[string]string)["Authorization"] != "Bearer ${secret:github_token}" {
		t.Errorf("gh token %v", e)
	}
	lin := preset(t, "linear")
	e = lin.EntryFor(MethodBrowser, EntryOptions{})
	if e["url"] != "https://mcp.linear.app/mcp/readonly" || strings.Join(e["oauth"].(*OAuthConfig).Scopes, " ") != "read" {
		t.Errorf("linear %v", e)
	}
	e = lin.EntryFor(MethodBrowser, EntryOptions{Writes: true})
	if e["url"] != "https://mcp.linear.app/mcp" || strings.Join(e["oauth"].(*OAuthConfig).Scopes, " ") != "read write" {
		t.Errorf("linear writes %v", e)
	}
	if o, _ := preset(t, "clickup").EntryFor(MethodBrowser, EntryOptions{})["oauth"].(*OAuthConfig); o == nil || strings.Join(o.Scopes, " ") != "read" {
		t.Errorf("clickup scopes %+v", o)
	}
	if o, _ := preset(t, "gitlab").EntryFor(MethodBrowser, EntryOptions{})["oauth"].(*OAuthConfig); o == nil || strings.Join(o.Scopes, " ") != "mcp" {
		t.Errorf("gitlab scopes %+v", o)
	}
	sen := preset(t, "sentry").EntryFor(MethodToken, EntryOptions{})
	if sen["headers"].(map[string]string)["Authorization"] != "Sentry-Bearer ${secret:sentry_token}" {
		t.Errorf("sentry token header %v", sen)
	}
	if preset(t, "clickup").Token != nil {
		t.Error("ClickUp has no token way")
	}
	sl := preset(t, "slack").EntryFor(MethodBrowser, EntryOptions{AppClientID: "1234.5678"})
	if o := sl["oauth"].(*OAuthConfig); o.ClientID != "1234.5678" || o.ClientSecret != "" {
		t.Errorf("slack %+v", o)
	}
	// Every entry parses as a valid user entry without literal secrets.
	for _, p := range Presets() {
		for _, m := range []Method{MethodCLI, MethodBrowser, MethodToken} {
			b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{p.Name: p.EntryFor(m, EntryOptions{GHUser: "a", AppClientID: "c"})}})
			list, warns, err := ParseFile(b, SourceUser, "p")
			if err != nil || len(warns) != 0 || list[0].Problem != "" {
				t.Errorf("%s/%s: %v %v %q", p.Name, m, err, warns, list[0].Problem)
			}
		}
	}
}

// Spec: the Slack app recipe: a manifest with the fixed localhost
// redirect and the read scopes; it holds no secret.
func TestSlackManifest(t *testing.T) {
	r := preset(t, "slack").App
	if r == nil {
		t.Fatal("no recipe")
	}
	man := r.Manifest()
	var v map[string]any
	if err := json.Unmarshal([]byte(man), &v); err != nil {
		t.Fatalf("manifest is not JSON: %v\n%s", err, man)
	}
	if !strings.Contains(man, "http://localhost:47391/callback") || !strings.Contains(man, "search:read.public") {
		t.Errorf("manifest %s", man)
	}
	for _, bad := range []string{"secret", "chat:write", "xox"} {
		if strings.Contains(man, bad) {
			t.Errorf("manifest has %q: %s", bad, man)
		}
	}
	if r.Port != 47391 || r.RedirectHost != "localhost" || len(r.Steps) < 3 {
		t.Errorf("recipe %+v", r)
	}
}

// Spec: the identity tool names the account; without one the label is
// the server's name; a failing call keeps the connection ("account
// unknown").
func TestIdentity(t *testing.T) {
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Login: "amin-jalali"})
	plain := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{
		"gh":    map[string]any{"type": "http", "url": srv.MCPURL()},
		"other": map[string]any{"type": "http", "url": plain.MCPURL()},
	})
	m := e.httpManager(t, nil, nil, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id, err := m.Identity(ctx, "gh", "get_me")
	if err != nil || id.Account != "amin-jalali" || id.Tools != 11 {
		t.Errorf("gh identity %+v %v", id, err)
	}
	id, err = m.Identity(ctx, "other", "get_me")
	if err != nil || id.Account != "" || id.Server != "fake-mcp-http" || id.Tools != 10 {
		t.Errorf("other identity %+v %v", id, err)
	}
	id, err = m.Identity(ctx, "other", "")
	if err != nil || id.Server != "fake-mcp-http" {
		t.Errorf("no identity tool %+v %v", id, err)
	}
	// The call fails (unknown tool on purpose): still connected.
	id, err = m.Identity(ctx, "gh", "fails")
	if err != nil || id.Account != "" || id.Tools != 11 {
		t.Errorf("failing identity %+v %v", id, err)
	}
}

// Spec (§6 prompt injection): an account label is cut to 64 cells of
// [A-Za-z0-9 ._@-].
func TestCleanAccount(t *testing.T) {
	for in, want := range map[string]string{
		"amin-jalali":                    "amin-jalali",
		"Ignore previous\ninstructions!": "Ignore previousinstructions",
		"a@b.c":                          "a@b.c",
		strings.Repeat("x", 90):          strings.Repeat("x", 64),
		"<script>":                       "script",
	} {
		if got := CleanAccount(in); got != want {
			t.Errorf("CleanAccount(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec: the account label is kept next to the tokens in
// credentials.json (mcp_oauth.<name>.account, or mcp_accounts for the
// gh / token ways), never in mcp.json.
func TestSaveAccount(t *testing.T) {
	dir := t.TempDir()
	cs := CredStore{Path: filepath.Join(dir, "ai", "credentials.json")}
	if err := cs.SetAccount("gh", "amin-jalali"); err != nil {
		t.Fatal(err)
	}
	if cs.Account("gh") != "amin-jalali" {
		t.Errorf("account %q", cs.Account("gh"))
	}
	if err := cs.Save("lin", mcpOAuthFor("https://mcp.linear.app/mcp")); err != nil {
		t.Fatal(err)
	}
	_ = cs.SetAccount("lin", "Acme")
	if e, _ := cs.Load("lin"); e.Account != "Acme" || cs.Account("lin") != "Acme" {
		t.Errorf("oauth account %+v", e)
	}
	_ = cs.Delete("lin")
	_ = cs.SetAccount("gh", "")
	if cs.Account("gh") != "" || cs.Account("lin") != "" {
		t.Error("accounts kept")
	}
}

// Spec: the preset seam for tests swaps endpoints and restores them.
func TestOverridePresets(t *testing.T) {
	restore := OverridePresets(func(ps []Preset) []Preset {
		for i := range ps {
			if ps[i].Name == "linear" {
				ps[i].URL = "http://127.0.0.1:1/mcp"
			}
		}
		return ps
	})
	if p, _ := PresetByName("linear"); p.URL != "http://127.0.0.1:1/mcp" {
		t.Errorf("override %s", p.URL)
	}
	restore()
	if p, _ := PresetByName("linear"); p.URL != "https://mcp.linear.app/mcp" {
		t.Errorf("restore %s", p.URL)
	}
}

// Spec: DeviceSignIn with a shipped app (GitHub style: errors as HTTP
// 200, no refresh token) gives tokens bound to the MCP server; for a
// user's own app id the endpoints come from discovery (GitLab).
func TestDeviceSignIn(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "gh-app", GitHubStyle: true, NoRefresh: true})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer(), Login: "amin-jalali"})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"github": map[string]any{"type": "http", "url": srv.MCPURL() + "/readonly"}})
	tk := &memTokens{}
	m := e.httpManager(t, nil, tk, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app := DeviceApp{DeviceEndpoint: as.Issuer() + "/device/code", TokenEndpoint: as.Issuer() + "/token",
		Issuer: as.Issuer(), Scopes: []string{"read:user"}, Resource: srv.MCPURL(), NoResourceParam: true}
	d, err := m.DeviceSignIn(ctx, "github", app, "gh-app")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := d.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FinishSignIn("github", tok); err != nil {
		t.Fatal(err)
	}
	id, err := m.Identity(ctx, "github", "get_me")
	if err != nil || id.Account != "amin-jalali" {
		t.Errorf("identity %+v %v", id, err)
	}
	if f := as.Forms(); f[len(f)-1].Get("resource") != "" {
		t.Error("resource sent to a server that does not take it")
	}

	gl := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "gl-app"})
	gsrv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: gl.Valid, AuthServer: gl.Issuer(), Scopes: []string{"mcp"}})
	e.write(t, e.user, map[string]any{"gitlab": map[string]any{"type": "http", "url": gsrv.MCPURL()}})
	m.Reload()
	d, err = m.DeviceSignIn(ctx, "gitlab", DeviceApp{}, "gl-app")
	if err != nil {
		t.Fatal(err)
	}
	if tok, err = d.Poll(ctx); err != nil || tok.Resource != gsrv.MCPURL() || tok.Scope != "mcp" {
		t.Fatalf("gitlab %+v %v", tok, err)
	}
	// No device endpoint at all: a clear error.
	plain := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	psrv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: plain.Valid, AuthServer: plain.Issuer()})
	e.write(t, e.user, map[string]any{"x": map[string]any{"type": "http", "url": psrv.MCPURL()}})
	m.Reload()
	if _, err := m.DeviceSignIn(ctx, "x", DeviceApp{}, "id"); err == nil || !strings.Contains(err.Error(), "device flow") {
		t.Errorf("no device: %v", err)
	}
}
