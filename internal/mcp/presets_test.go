package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (plan §1): presets for the servers people ask for, with the
// public endpoints from the plan. None holds a literal secret: tokens
// are ${secret:NAME}.
func TestPresets(t *testing.T) {
	want := map[string]string{
		"linear":    "https://mcp.linear.app/mcp",
		"github":    "https://api.githubcopilot.com/mcp/",
		"gitlab":    "https://gitlab.com/api/v4/mcp",
		"atlassian": "https://mcp.atlassian.com/v1/mcp",
		"slack":     "https://mcp.slack.com/mcp",
		"notion":    "https://mcp.notion.com/mcp",
		"sentry":    "https://mcp.sentry.dev/mcp",
		"clickup":   "https://mcp.clickup.com/mcp",
	}
	got := map[string]Preset{}
	for _, p := range Presets() {
		got[p.Name] = p
	}
	for name, url := range want {
		p, ok := got[name]
		if !ok {
			t.Errorf("no preset %s", name)
			continue
		}
		b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{name: p.Entry()}})
		list, warns, err := ParseFile(b, SourceUser, "p")
		if err != nil || len(warns) != 0 || len(list) != 1 || list[0].Problem != "" {
			t.Errorf("%s: %v %v %+v", name, err, warns, list)
			continue
		}
		if s := list[0]; s.Type != TypeHTTP || s.URL != url {
			t.Errorf("%s = %s %s, want http %s", name, s.Type, s.URL, url)
		}
		if p.Title == "" || p.Note == "" {
			t.Errorf("%s: no title or note", name)
		}
	}
	gh := got["github"].Entry()
	if h, _ := gh["headers"].(map[string]string); h["Authorization"] != "Bearer ${secret:github_pat}" {
		t.Errorf("github headers %v", gh["headers"])
	}
	if o, _ := got["gitlab"].Entry()["oauth"].(*OAuthConfig); o == nil || strings.Join(o.Scopes, " ") != "mcp" {
		t.Errorf("gitlab oauth %+v", got["gitlab"].Entry()["oauth"])
	}
	if o, _ := got["slack"].Entry()["oauth"].(*OAuthConfig); o == nil || !strings.Contains(o.ClientSecret, "${secret:") {
		t.Errorf("slack oauth %+v", got["slack"].Entry()["oauth"])
	}
}

// AddServer puts one entry into mcp.json (made when missing), keeps the
// other entries and keys, and refuses a name that exists.
func TestAddServer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg", "mcp.json")
	if err := AddServer(p, "linear", Presets()[0].Entry()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, strings.Replace(mustRead(t, p), `"mcpServers"`, `"other": 1, "mcpServers"`, 1))
	if err := AddServer(p, "notion", map[string]any{"type": "http", "url": "https://mcp.notion.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := AddServer(p, "notion", map[string]any{"url": "https://x.example/mcp"}); err == nil {
		t.Error("a second notion was added")
	}
	list, _ := Load(p, "")
	if len(list) != 2 || list[1].URL != "https://mcp.notion.com/mcp" {
		t.Errorf("servers %+v", list)
	}
	if !strings.Contains(mustRead(t, p), `"other": 1`) {
		t.Errorf("other key lost: %s", mustRead(t, p))
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
