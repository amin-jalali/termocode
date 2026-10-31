package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// MCPX-28: an entry copied for a teammate keeps its shape and its
// ${secret:NAME} references, turns literal tokens into new references
// (never the value), and drops the personal "auth" sign-in.
func TestTeammateJSON(t *testing.T) {
	tok := "ghp_" + "test0123456789abcdefABCDEF"
	known := "plain-but-" + "stored-secret-42"
	body := `{"mcpServers":{"gh":{"type":"http","url":"https://api.example/mcp",
		"headers":{"Authorization":"Bearer ` + tok + `","X-Ref":"${secret:gh_key}","X-Plain":"yes"},
		"auth":{"gh":"octo"},"autoApprove":["echo"]},
		"notes":{"command":"notes-mcp","args":["--token","` + tok + `"],"env":{"LOG":"debug","NOTES_KEY":"` + known + `"},
		"oauth":{"client_id":"abc","client_secret":"sk-` + `test-client-secret"}}}}`
	list, _, err := ParseFile([]byte(body), SourceUser, "/x/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	isKnown := func(v string) bool { return strings.Contains(v, known) }
	for _, s := range list {
		out, names, err := TeammateJSON(s, isKnown)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{tok, known, "client-secret", "octo"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: copy has %q:\n%s", s.Name, bad, out)
			}
		}
		var f struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(out), &f); err != nil || f.MCPServers[s.Name] == nil {
			t.Fatalf("%s: not an mcpServers block: %v\n%s", s.Name, err, out)
		}
		back, _, err := ParseFile([]byte(out), SourceUser, "/y/mcp.json")
		if err != nil || len(back) != 1 || back[0].Problem != "" {
			t.Fatalf("%s: the copy does not load: %v %+v", s.Name, err, back)
		}
		switch s.Name {
		case "gh":
			h := back[0].Headers
			if h["Authorization"] != "Bearer ${secret:gh_authorization}" || h["X-Ref"] != "${secret:gh_key}" || h["X-Plain"] != "yes" || back[0].Auth != nil {
				t.Errorf("gh = %+v auth %+v", h, back[0].Auth)
			}
			if strings.Join(names, ",") != "gh_authorization,gh_key" {
				t.Errorf("gh names = %v", names)
			}
		case "notes":
			e := back[0]
			if e.Env["LOG"] != "debug" || e.Env["NOTES_KEY"] != "${secret:notes_notes_key}" ||
				e.Args[1] != "${secret:notes_arg_2}" || e.OAuth.ClientSecret != "${secret:notes_client_secret}" {
				t.Errorf("notes = %+v %+v", e, e.OAuth)
			}
		}
	}
}
