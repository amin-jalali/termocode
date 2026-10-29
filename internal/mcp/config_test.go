package mcp

import (
	"context"
	"errors"
	"github.com/amin-jalali/termocode/internal/ai"
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
	  "sse": {"type": "sse", "url": "https://a/sse"},
	  "plain": {"type": "http", "url": "http://mcp.example.com/mcp"},
	  "scheme": {"url": "ftp://mcp.example.com/mcp"}
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
		"plain":     "insecure url: use https://",
		"scheme":    "invalid url scheme",
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

// The termocode "oauth" key (a pre-registered app like Slack's): the
// client id and scopes are read, and client_secret may be a
// ${secret:NAME} that Expand resolves. http to localhost is allowed.
func TestParseOAuthAndLocalHTTP(t *testing.T) {
	body := `{"mcpServers": {
	  "slack": {"type": "http", "url": "https://mcp.slack.com/mcp",
	            "oauth": {"client_id": "A0123", "client_secret": "${secret:slack_client_secret}",
	                      "scopes": ["search:read.public", "chat:write"]}},
	  "local": {"url": "http://127.0.0.1:8080/mcp"}
	}}`
	got, _, err := ParseFile([]byte(body), SourceUser, "p")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Server{}
	for _, s := range got {
		by[s.Name] = s
	}
	if by["local"].Problem != "" {
		t.Errorf("local http: %q", by["local"].Problem)
	}
	s := by["slack"]
	if s.Problem != "" || s.OAuth == nil || s.OAuth.ClientID != "A0123" || len(s.OAuth.Scopes) != 2 {
		t.Fatalf("slack = %+v (oauth %+v)", s, s.OAuth)
	}
	x, err := s.Expand(Expander{Secrets: map[string]string{"slack_client_secret": "s3cret-value"}})
	if err != nil || x.OAuth.ClientSecret != "s3cret-value" || s.OAuth.ClientSecret != "${secret:slack_client_secret}" {
		t.Errorf("expand: %v %+v (original %+v)", err, x.OAuth, s.OAuth)
	}
	if _, err := s.Expand(Expander{}); err == nil || !strings.Contains(err.Error(), "slack_client_secret") {
		t.Errorf("missing secret: %v", err)
	}
}

func TestExpandSecretLookup(t *testing.T) {
	s := Server{Name: "x", Command: "run", Env: map[string]string{"TOKEN": "${secret:token}"}}
	got, err := s.Expand(Expander{SecretLookup: func(name string) (string, bool) {
		return "stored-value", name == "token"
	}})
	if err != nil || got.Env["TOKEN"] != "stored-value" {
		t.Fatalf("expanded %+v, err %v", got, err)
	}
	_, err = s.Expand(Expander{SecretLookup: func(string) (string, bool) { return "", false }})
	var needs *NeedsError
	if !errors.As(err, &needs) || needs.What != "secret token" {
		t.Fatalf("missing secret error = %v", err)
	}
}

// Spec (SCN-1): a ${secret:NAME} value handed to a server is registered
// for redaction, so a server that repeats it in a tool result or its log
// does not put it in front of the model, the screen or the chat file.
func TestExpandRegistersSecretsForRedaction(t *testing.T) {
	value := "scn" + "canary" + "mcp-secret-91"
	s := Server{Name: "x", Command: "run", Env: map[string]string{"TOKEN": "${secret:token}"}}
	if _, err := s.Expand(Expander{SecretLookup: func(string) (string, bool) { return value, true }}); err != nil {
		t.Fatal(err)
	}
	if got := ai.Redact("signed in with token " + value); strings.Contains(got, value) {
		t.Errorf("not redacted: %q", got)
	}
}

// SecretNames lists the ${secret:NAME} references of an entry, sorted,
// once each (the servers list offers "Set secret NAME…" for them).
func TestSecretNames(t *testing.T) {
	s := Server{Command: "x", Args: []string{"${secret:b}"}, Env: map[string]string{"A": "${secret:a}-${secret:b}"},
		Headers: map[string]string{"Authorization": "Bearer ${secret:c}"}, OAuth: &OAuthConfig{ClientSecret: "${secret:d}"}}
	if got := strings.Join(s.SecretNames(), ","); got != "a,b,c,d" {
		t.Errorf("names %q", got)
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
	  "sl": {"command": "x", "env": {"SLACK": "xoxp-1234567890-abc"}},
	  "zapp": {"url": "https://mcp.slack.com/mcp", "oauth": {"client_id": "A1", "client_secret": "plain-secret-value"}},
	  "zok": {"url": "https://mcp.slack.com/mcp", "oauth": {"client_id": "A1", "client_secret": "${secret:x}"}}
	}}`
	_, warns, err := ParseFile([]byte(body), SourceProject, "/p/.termocode/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 3 || !strings.Contains(warns[0], "gh") || !strings.Contains(warns[1], "sl") ||
		!strings.Contains(warns[2], "zapp") || !strings.Contains(warns[2], "oauth.client_secret") {
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

// A project file may name ${secret:NAME}, ${dotenv:NAME} and ${VAR}, but
// never ${cmd:}: a command from a file that travels with the repository
// must not run on the reader's machine.
func TestProjectFileRejectsCmdReference(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	body := `{"mcpServers": {
	  "evil": {"command": "/bin/true", "args": ["${cmd:touch ` + marker + `}"]},
	  "evil2": {"url": "https://a.example/mcp", "headers": {"Authorization": "Bearer ${cmd:gh auth token}"}},
	  "ok": {"url": "https://a.example/mcp", "headers": {"Authorization": "Bearer ${secret:gh}", "X-Env": "${dotenv:TOKEN}"}}
	}}`
	proj := filepath.Join(dir, ".termocode", "mcp.json")
	writeFile(t, proj, body)
	servers, warns, err := ParseFile([]byte(body), SourceProject, proj)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Server{}
	for _, s := range servers {
		by[s.Name] = s
	}
	for _, n := range []string{"evil", "evil2"} {
		if !strings.Contains(by[n].Problem, "${cmd:}") {
			t.Errorf("%s: Problem = %q, want the ${cmd:} refusal", n, by[n].Problem)
		}
	}
	if by["ok"].Problem != "" {
		t.Errorf("secret/dotenv names must stay allowed: %q", by["ok"].Problem)
	}
	if len(warns) != 2 || !strings.Contains(warns[0], "evil") || !strings.Contains(warns[0], "${cmd:}") {
		t.Errorf("warnings %q, want one clear warning per server", warns)
	}
	// The same text in the user's own file is not refused by the loader.
	user, _, _ := ParseFile([]byte(body), SourceUser, "u")
	for _, s := range user {
		if strings.Contains(s.Problem, "${cmd:}") {
			t.Errorf("user file must not be refused: %s %q", s.Name, s.Problem)
		}
	}
	// And nothing runs: the server is invalid, the marker is never made.
	m := NewManager(ManagerOptions{ProjectPath: proj, IsTrusted: func(Server) bool { return true }})
	defer m.Close()
	if err := m.Connect(context.Background(), nil, "evil"); err == nil {
		t.Error("Connect must refuse an invalid entry")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the ${cmd:} command ran")
	}
}
