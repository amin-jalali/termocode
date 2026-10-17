package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (plan CX-12, "Paste a config…"): a Claude / Cursor block is read
// in its three usual shapes; literal tokens move out of the entries into
// secrets (${secret:NAME}); bad entries are reported, not added.
func TestParseImport(t *testing.T) {
	full := `{"mcpServers":{
		"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ghp_abcdefghijklmnopqrstuvwxyz123456"}},
		"pg":{"command":"npx","args":["-y","pg-mcp"],"env":{"PG_URL":"postgres://me@localhost/db","API_KEY":"sk-proj-abcdefghijklmnopqrstuv"}},
		"bad":{"type":"http","url":"http://evil.example/mcp"}}}`
	list, problems, err := ParseImport(full)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Imported{}
	for _, im := range list {
		by[im.Name] = im
	}
	gh := by["github"]
	b, _ := json.Marshal(gh.Entry)
	if !strings.Contains(string(b), "Bearer ${secret:github_authorization}") || strings.Contains(string(b), "ghp_") {
		t.Errorf("github entry %s", b)
	}
	if gh.Secrets["github_authorization"] != "ghp_abcdefghijklmnopqrstuvwxyz123456" {
		t.Errorf("github secrets %v", gh.Secrets)
	}
	pg := by["pg"]
	b, _ = json.Marshal(pg.Entry)
	if !strings.Contains(string(b), "${secret:pg_api_key}") || strings.Contains(string(b), "sk-proj") || !strings.Contains(string(b), "postgres://me@localhost/db") {
		t.Errorf("pg entry %s", b)
	}
	if _, ok := by["bad"]; ok || len(problems) != 1 || !strings.Contains(problems[0], "bad") {
		t.Errorf("bad entry: %v %v", by["bad"], problems)
	}
	// Just the servers map, and one "name": {…} pair.
	for _, txt := range []string{
		`{"linear":{"type":"http","url":"https://mcp.linear.app/mcp"}}`,
		`"linear": {"type": "http", "url": "https://mcp.linear.app/mcp"}`,
		`"linear": {"type": "http", "url": "https://mcp.linear.app/mcp"},`,
	} {
		list, _, err := ParseImport(txt)
		if err != nil || len(list) != 1 || list[0].Name != "linear" {
			t.Errorf("%s: %v %+v", txt, err, list)
		}
	}
	if _, _, err := ParseImport("hello"); err == nil {
		t.Error("not JSON accepted")
	}
}
