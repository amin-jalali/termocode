package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The shapes people paste from Claude Desktop, Claude Code and Cursor
// all parse; type defaults from command / url; "transport" is an alias.
func TestParseClaudeAndCursorShapes(t *testing.T) {
	body := `{
	  "mcpServers": {
	    "fs": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]},
	    "linear": {"type": "http", "url": "https://mcp.linear.app/mcp"},
	    "cursor": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer ${GH}"}},
	    "desk": {"transport": "streamable-http", "url": "https://x.example/mcp"},
	    "pg": {"command": "uvx", "args": ["postgres-mcp"], "env": {"DATABASE_URI": "${secret:pg_uri}"},
	           "readOnly": true, "timeout": 120, "autoApprove": ["explain_query"], "deny": ["execute_sql"]}
	  },
	  "otherKey": 1
	}`
	got, warns, err := ParseFile([]byte(body), SourceUser, "/u/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %q", warns)
	}
	by := map[string]Server{}
	for _, s := range got {
		by[s.Name] = s
	}
	if s := by["fs"]; s.Type != TypeStdio || s.Command != "npx" || len(s.Args) != 3 || s.Source != SourceUser {
		t.Errorf("fs = %+v", s)
	}
	for _, n := range []string{"linear", "cursor", "desk"} {
		if by[n].Type != TypeHTTP {
			t.Errorf("%s type = %q, want http", n, by[n].Type)
		}
	}
	pg := by["pg"]
	if !pg.ReadOnly || pg.Timeout != 120 || !reflect.DeepEqual(pg.AutoApprove, []string{"explain_query"}) ||
		!reflect.DeepEqual(pg.Deny, []string{"execute_sql"}) || pg.Env["DATABASE_URI"] != "${secret:pg_uri}" {
		t.Errorf("pg = %+v", pg)
	}
	if names := []string{got[0].Name, got[len(got)-1].Name}; names[0] != "cursor" || names[1] != "pg" {
		t.Errorf("not sorted by name: %v", names)
	}
}

// Invalid entries are kept with a Problem (shown in the servers list),
// never an error for the whole file.
func TestParseValidation(t *testing.T) {
	body := `{"mcpServers": {
	  "both": {"command": "x", "url": "https://a"},
	  "none": {},
	  "bad name!": {"command": "x"},
	  "sse": {"type": "sse", "url": "https://a/sse"}
	}}`
	got, _, err := ParseFile([]byte(body), SourceUser, "p")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"both":      "url and command both set",
		"none":      "needs command or url",
		"bad name!": "name may only use letters, digits, - and _",
		"sse":       "sse transport is not supported yet",
	}
	for _, s := range got {
		if !strings.Contains(s.Problem, want[s.Name]) || want[s.Name] == "" {
			t.Errorf("%s problem = %q, want %q", s.Name, s.Problem, want[s.Name])
		}
	}
	if _, _, err := ParseFile([]byte(`{nope`), SourceUser, "p"); err == nil {
		t.Error("bad JSON: want an error")
	}
}

// A project entry overrides the user entry of the same name;
// "disabled": true in the project hides a user server there.
func TestLoadMerge(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user", "mcp.json")
	proj := filepath.Join(dir, "proj", ".termocode", "mcp.json")
	writeFile(t, user, `{"mcpServers": {
		"a": {"command": "user-a"},
		"b": {"command": "user-b"},
		"c": {"command": "user-c"}}}`)
	writeFile(t, proj, `{"mcpServers": {
		"b": {"command": "proj-b"},
		"c": {"disabled": true},
		"d": {"command": "proj-d"}}}`)
	got, warns := Load(user, proj)
	if len(warns) != 0 {
		t.Errorf("warnings %q", warns)
	}
	var lines []string
	for _, s := range got {
		lines = append(lines, s.Name+"="+s.Command+"/"+s.Source)
	}
	want := "a=user-a/user b=proj-b/project d=proj-d/project"
	if strings.Join(lines, " ") != want {
		t.Errorf("merged %q, want %q", strings.Join(lines, " "), want)
	}
	// Missing files are fine.
	if got, warns := Load(filepath.Join(dir, "no1"), filepath.Join(dir, "no2")); len(got) != 0 || len(warns) != 0 {
		t.Errorf("missing files: %v %v", got, warns)
	}
	// A broken file is a warning, the other file still loads.
	writeFile(t, proj, `{broken`)
	got, warns = Load(user, proj)
	if len(got) != 3 || len(warns) != 1 || !strings.Contains(warns[0], proj) {
		t.Errorf("broken project file: %d servers, warnings %q", len(got), warns)
	}
}

func TestExpand(t *testing.T) {
	x := Expander{
		Env:       envMap{"GH": "ghp_secretvalue123", "EMPTY": ""}.lookup,
		Secrets:   map[string]string{"pg_uri": "postgres://u:p@h/db"},
		Workspace: "/work",
		Home:      "/home/me",
	}
	s := Server{Name: "x", Type: TypeStdio, Command: "${userHome}/bin/srv",
		Args: []string{"${workspaceFolder}", "--level=${LEVEL:-info}", "${GH}"},
		Env:  map[string]string{"DB": "${secret:pg_uri}", "E": "${EMPTY:-d}"},
		Cwd:  "${workspaceFolder}/sub",
	}
	got, err := s.Expand(x)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "/home/me/bin/srv" || got.Cwd != "/work/sub" ||
		!reflect.DeepEqual(got.Args, []string{"/work", "--level=info", "ghp_secretvalue123"}) ||
		got.Env["DB"] != "postgres://u:p@h/db" || got.Env["E"] != "d" {
		t.Errorf("expanded %+v", got)
	}
	if s.Args[0] != "${workspaceFolder}" {
		t.Error("Expand changed the original")
	}

	// An unset variable without a default is a clear "needs" error.
	_, err = Server{Name: "y", Command: "x", Args: []string{"${NOPE}"}}.Expand(x)
	if err == nil || err.Error() != "needs ${NOPE}" {
		t.Errorf("unset var: %v", err)
	}
	_, err = Server{Name: "y", Command: "x", Env: map[string]string{"K": "${secret:missing}"}}.Expand(x)
	if err == nil || err.Error() != "needs secret missing" {
		t.Errorf("missing secret: %v", err)
	}
}

// A literal value that looks like a token is warned about (it should be a
// ${secret:…} or ${VAR} reference).
func TestSecretLiteralWarning(t *testing.T) {
	body := `{"mcpServers": {
	  "gh": {"url": "https://api.githubcopilot.com/mcp/", "headers": {"Authorization": "Bearer ghp_abcdefghijklmnopqrstuvwxyz0123"}},
	  "ok": {"url": "https://a", "headers": {"Authorization": "Bearer ${secret:gh}"}},
	  "sl": {"command": "x", "env": {"SLACK": "xoxp-1234567890-abc"}}
	}}`
	_, warns, err := ParseFile([]byte(body), SourceProject, "/p/.termocode/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 2 || !strings.Contains(warns[0], "gh") || !strings.Contains(warns[1], "sl") {
		t.Errorf("warnings %q", warns)
	}
}

// Hash changes when the entry changes and only then.
func TestHash(t *testing.T) {
	a, _, _ := ParseFile([]byte(`{"mcpServers": {"a": {"command": "x", "args": ["1"]}}}`), SourceUser, "p")
	b, _, _ := ParseFile([]byte(`{"mcpServers": {"a": {"args": ["1"],   "command": "x"}}}`), SourceUser, "p")
	c, _, _ := ParseFile([]byte(`{"mcpServers": {"a": {"command": "x", "args": ["2"]}}}`), SourceUser, "p")
	if a[0].Hash() != b[0].Hash() {
		t.Error("same entry, other key order: hash differs")
	}
	if a[0].Hash() == c[0].Hash() {
		t.Error("changed entry: same hash")
	}
}

type envMap map[string]string

func (m envMap) lookup(k string) (string, bool) { v, ok := m[k]; return v, ok }
