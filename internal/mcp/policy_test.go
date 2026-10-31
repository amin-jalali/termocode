package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

func withSystemPolicy(t *testing.T, path string) {
	t.Helper()
	old := SystemPolicyPath
	SystemPolicyPath = path
	t.Cleanup(func() { SystemPolicyPath = old })
}

// A policy file allows or denies servers by name, command or url. A
// denied entry stays listed with "blocked by policy" and never runs.
func TestPolicyDenyAndAllow(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "cfg", "mcp.json")
	writeFile(t, user, `{"mcpServers": {
	  "fs": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem"]},
	  "evil": {"command": "curl"},
	  "linear": {"url": "https://mcp.linear.app/mcp"},
	  "other": {"url": "https://mcp.other.example/mcp"}
	}}`)
	sys := filepath.Join(dir, "etc", "mcp-policy.json")
	withSystemPolicy(t, sys)
	writeFile(t, sys, `{"deny": [{"command": "curl"}],
	  "allow": [{"command": "npx"}, {"url": "https://mcp.linear.app/*"}, {"name": "evil"}]}`)
	got, warns := LoadAll(user, "")
	if len(warns) != 0 {
		t.Errorf("warnings %q", warns)
	}
	var lines []string
	for _, s := range got {
		lines = append(lines, s.Name+":"+s.Problem)
	}
	want := "evil:blocked by policy (" + sys + ") fs: linear: other:blocked by policy (" + sys + ")"
	if strings.Join(lines, " ") != want {
		t.Errorf("got  %q\nwant %q", strings.Join(lines, " "), want)
	}
	// The user's own policy next to mcp.json adds its denies.
	writeFile(t, filepath.Join(dir, "cfg", "mcp-policy.json"), `{"deny": [{"name": "lin*"}]}`)
	got, _ = LoadAll(user, "")
	for _, s := range got {
		if s.Name == "linear" && !strings.HasPrefix(s.Problem, "blocked by policy") {
			t.Errorf("user deny: linear problem %q", s.Problem)
		}
	}
}

// A policy file that can not be read blocks every server (fail closed)
// and says why; no policy file blocks nothing.
func TestPolicyBrokenFileBlocksAll(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	writeFile(t, user, `{"mcpServers": {"fs": {"command": "npx"}}}`)
	withSystemPolicy(t, filepath.Join(dir, "none.json"))
	if got, _ := LoadAll(user, ""); got[0].Problem != "" {
		t.Errorf("no policy: problem %q", got[0].Problem)
	}
	sys := filepath.Join(dir, "bad.json")
	withSystemPolicy(t, sys)
	writeFile(t, sys, `{nope`)
	got, warns := LoadAll(user, "")
	if !strings.HasPrefix(got[0].Problem, "blocked by policy") || len(warns) != 1 {
		t.Errorf("broken policy: problem %q warns %q", got[0].Problem, warns)
	}
}
