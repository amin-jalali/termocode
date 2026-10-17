//go:build unix

package testkit

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Remote MCP servers (MC-5 / MC-6): a fake Streamable HTTP server and a
// fake OAuth authorization server, both httptest on 127.0.0.1. The test
// plays the browser.

// remoteDriver starts the app with a user mcp.json, optional
// credentials.json and, when ssh is set, SSH_CONNECTION (the browser is
// on another machine).
func remoteDriver(t *testing.T, aiURL, mcpJSON, creds string, ssh bool) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	cfg := map[string]string{"mcp.json": mcpJSON}
	if creds != "" {
		cfg["ai/credentials.json"] = creds
	}
	return New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(), Config: cfg,
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", aiURL+"/v1")
			if ssh {
				t.Setenv("SSH_CONNECTION", "10.1.2.3 50000 10.9.8.7 22")
			} else {
				t.Setenv("SSH_CONNECTION", "")
			}
		}})
}

func credentials(t *testing.T) (map[string]any, os.FileMode) {
	t.Helper()
	p := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "ai", "credentials.json")
	st, err := os.Stat(p)
	if err != nil {
		return nil, 0
	}
	b, _ := os.ReadFile(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, st.Mode().Perm()
}

func signedIn(t *testing.T, name string) bool {
	c, _ := credentials(t)
	o, _ := c["mcp_oauth"].(map[string]any)
	_, ok := o[name]
	return ok
}

// openServerAction opens AI: MCP Servers…, the server, and runs one of
// its actions by title.
func openServerAction(d *Driver, server, action string) {
	d.T.Helper()
	runPalette(d, "AI: MCP Servers")
	if !d.WaitForText(server, 5*time.Second) {
		d.T.Fatalf("no server %s:\n%s", server, screen(d))
	}
	d.Type(server)
	d.Key("enter")
	if !d.WaitForText(action, 5*time.Second) {
		d.T.Fatalf("no action %q:\n%s", action, screen(d))
	}
	d.Type(action)
	d.Key("enter")
}

// Spec (MC-6): AI: MCP Servers… → Sign in opens the browser URL (also
// copied); the browser comes back to the loopback listener; the server
// connects, its sign-in is in credentials.json (0600) and the chat can
// use its tools. Sign out forgets it.
func TestAIMCPRemoteSignInBehavior(t *testing.T) {
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer(), Session: true})
	s, model := fakeToolModel(t, toolCall{"mcp__lin__echo", map[string]any{"text": "remote"}})
	d := remoteDriver(t, model.URL, `{"mcpServers":{"lin":{"type":"http","url":"`+srv.MCPURL()+`"}}}`, "", false)

	openServerAction(d, "lin", "Sign in")
	if !d.WaitForText("Sign in to MCP lin", 10*time.Second) {
		t.Fatalf("no sign-in prompt:\n%s", screen(d))
	}
	var authURL string
	if !d.WaitFor(func() bool { authURL, _ = clip.get(); return strings.HasPrefix(authURL, as.Issuer()+"/authorize?") }, 5*time.Second) {
		t.Fatalf("authorize URL not copied: %q", authURL)
	}
	resp, err := http.Get(authURL) // the browser
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "close this tab") {
		t.Errorf("callback page %q", page)
	}
	if !d.WaitForText("lin: signed in", 10*time.Second) {
		t.Fatalf("no signed-in toast:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Sign in to MCP lin") {
		t.Error("the sign-in prompt stayed open")
	}
	if c, mode := credentials(t); !signedIn(t, "lin") || mode != 0o600 {
		t.Errorf("credentials (mode %o): %v", mode, c)
	}

	askAI(d, "echo remote")
	waitDone(t, d)
	if r := s.toolResults(); len(r) != 1 || !strings.HasPrefix(r[0], "echo: remote") {
		t.Errorf("tool results %q", r)
	}

	openServerAction(d, "lin", "Sign out")
	if !d.WaitFor(func() bool { return !signedIn(t, "lin") }, 5*time.Second) {
		t.Error("Sign out kept the tokens")
	}
}

// Spec (SSH): with SSH_CONNECTION set, the prompt explains the paste way
// and the ssh -L command; pasting the address the browser shows
// completes the sign-in.
func TestAIMCPRemoteSignInPasteOverSSHBehavior(t *testing.T) {
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{"lin":{"type":"http","url":"`+srv.MCPURL()+`"}}}`, "", true)

	openServerAction(d, "lin", "Sign in")
	if !d.WaitForText("Sign in to MCP lin", 10*time.Second) {
		t.Fatalf("no sign-in prompt:\n%s", screen(d))
	}
	sc := screen(d)
	if !strings.Contains(sc, "paste") || !strings.Contains(sc, "ssh -L") || !strings.Contains(sc, "10.9.8.7") {
		t.Errorf("prompt does not explain the SSH ways:\n%s", sc)
	}
	authURL, _ := clip.get()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	callback := resp.Header.Get("Location") // what the laptop's browser shows
	if !strings.Contains(callback, "/callback?") {
		t.Fatalf("callback %q", callback)
	}
	d.Type(callback)
	d.Key("enter")
	if !d.WaitForText("lin: signed in", 10*time.Second) {
		t.Fatalf("paste did not sign in:\n%s", screen(d))
	}
	if !signedIn(t, "lin") {
		t.Error("no tokens saved")
	}
}

// Spec: AI: MCP Servers… starts with "+ Add a server…"; it lists the
// presets (Linear, GitHub, GitLab, Atlassian, Slack, Notion, Sentry,
// ClickUp); picking one writes it into the user mcp.json and opens its
// actions with Sign in. Nothing connects by itself.
func TestAIMCPPresetsBehavior(t *testing.T) {
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)
	runPalette(d, "AI: MCP Servers")
	if !d.WaitForText("+ Add a server", 5*time.Second) {
		t.Fatalf("no add row:\n%s", screen(d))
	}
	d.Type("Add a server")
	d.Key("enter")
	for _, p := range []string{"Linear", "GitHub", "GitLab", "Atlassian", "Slack", "Notion", "Sentry", "ClickUp"} {
		if !d.WaitForText(p, 5*time.Second) {
			t.Fatalf("no preset %s:\n%s", p, screen(d))
		}
	}
	d.Type("Notion")
	d.Key("enter")
	if !d.WaitForText("Sign in", 5*time.Second) {
		t.Fatalf("no actions after adding:\n%s", screen(d))
	}
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "mcp.json"))
	if !strings.Contains(string(b), `"notion"`) || !strings.Contains(string(b), "https://mcp.notion.com/mcp") {
		t.Errorf("mcp.json: %s", b)
	}
}

// Spec (MC-5): a remote server with a PAT from ${secret:NAME} runs its
// tools; the token never shows on screen, in the MCP log or errors.log.
func TestAIMCPRemotePATBehavior(t *testing.T) {
	const pat = "ghp_patvalue0123456789abcdefXYZ"
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: pat, SSE: true})
	s, model := fakeToolModel(t,
		toolCall{"mcp__gh__echo", map[string]any{"text": "pat"}},
		toolCall{"mcp__gh__fails", map[string]any{}})
	d := remoteDriver(t, model.URL,
		`{"mcpServers":{"gh":{"type":"http","url":"`+srv.MCPURL()+`","headers":{"Authorization":"Bearer ${secret:gh_pat}"}}}}`,
		`{"mcp_secrets":{"gh_pat":"`+pat+`"}}`, false)
	askAI(d, "echo with the PAT")
	waitDone(t, d)
	if r := s.toolResults(); len(r) != 2 || !strings.HasPrefix(r[0], "echo: pat") {
		t.Fatalf("tool results %q", r)
	}
	if got := srv.Requests()[0].Header.Get("Authorization"); got != "Bearer "+pat {
		t.Errorf("Authorization sent %q", got)
	}
	runPalette(d, "AI: MCP: Show Log")
	if !d.WaitForText("gh: connected", 5*time.Second) {
		t.Errorf("MCP log:\n%s", screen(d))
	}
	if strings.Contains(screen(d), pat) {
		t.Errorf("PAT on screen:\n%s", screen(d))
	}
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "errors.log"))
	if strings.Contains(string(b), pat) {
		t.Error("PAT in errors.log")
	}
	runPalette(d, "AI: MCP Servers")
	if !d.WaitForText("remote · connected", 5*time.Second) {
		t.Errorf("servers list:\n%s", screen(d))
	}
}
