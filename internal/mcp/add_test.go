package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// entryJSON is an entry as compact JSON (sorted keys), for comparisons.
func entryJSON(t *testing.T, e map[string]any) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLocalSpecEntryCommand(t *testing.T) {
	e, err := LocalSpec{Name: "files", Command: "my-server", Args: []string{"--root", "/srv"},
		Env: map[string]string{"LOG": "debug"}, Secrets: []string{"API_KEY"}}.Entry()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"args":["--root","/srv"],"command":"my-server","env":{"API_KEY":"${secret:files_api_key}","LOG":"debug"}}`
	if got := entryJSON(t, e); got != want {
		t.Fatalf("entry\n got %s\nwant %s", got, want)
	}
}

func TestLocalSpecEntryPackages(t *testing.T) {
	cases := []struct {
		spec LocalSpec
		want string
	}{
		{LocalSpec{Name: "fs", Kind: KindNPM, Command: "@scope/server-fs", Args: []string{"/tmp"}},
			`{"args":["-y","@scope/server-fs","/tmp"],"command":"npx"}`},
		{LocalSpec{Name: "git", Kind: KindPip, Command: "mcp-server-git"},
			`{"args":["mcp-server-git"],"command":"uvx"}`},
		{LocalSpec{Name: "pg", Kind: KindImage, Command: "mcp/postgres:1", Env: map[string]string{"MODE": "ro"}, Secrets: []string{"PGPASSWORD"}},
			`{"args":["run","-i","--rm","-e","MODE","-e","PGPASSWORD","mcp/postgres:1"],"command":"docker","env":{"MODE":"ro","PGPASSWORD":"${secret:pg_pgpassword}"}}`},
	}
	for _, c := range cases {
		e, err := c.spec.Entry()
		if err != nil {
			t.Fatalf("%s: %v", c.spec.Name, err)
		}
		if got := entryJSON(t, e); got != c.want {
			t.Fatalf("%s\n got %s\nwant %s", c.spec.Name, got, c.want)
		}
		b, _ := json.Marshal(e)
		if s := parseEntry(c.spec.Name, b); s.Problem != "" || s.Type != TypeStdio {
			t.Fatalf("%s: parsed as %q / %q", c.spec.Name, s.Type, s.Problem)
		}
	}
}

func TestLocalSpecEntryRefusesBadInput(t *testing.T) {
	tok := "sk-" + "test0123456789abcdef"
	cases := map[string]LocalSpec{
		"name":           {Name: "bad name", Command: "x"},
		"no command":     {Name: "a"},
		"kind":           {Name: "a", Kind: "gem", Command: "x"},
		"package dash":   {Name: "a", Kind: KindNPM, Command: "-y"},
		"package space":  {Name: "a", Kind: KindPip, Command: "a b"},
		"env name":       {Name: "a", Command: "x", Env: map[string]string{"1BAD": "v"}},
		"secret name":    {Name: "a", Command: "x", Secrets: []string{"A-B"}},
		"literal token":  {Name: "a", Command: "x", Env: map[string]string{"TOKEN": tok}},
		"env and secret": {Name: "a", Command: "x", Env: map[string]string{"K": "v"}, Secrets: []string{"K"}},
	}
	for what, spec := range cases {
		e, err := spec.Entry()
		if err == nil {
			t.Fatalf("%s: no error (entry %v)", what, e)
		}
		if strings.Contains(err.Error(), tok) {
			t.Fatalf("%s: the error shows the token: %v", what, err)
		}
	}
}

func TestLocalSecretNamesMatchEntry(t *testing.T) {
	spec := LocalSpec{Name: "My-Srv", Command: "x", Secrets: []string{"API_KEY", "OTHER"}}
	got := spec.SecretNames()
	if want := []string{"my-srv_api_key", "my-srv_other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names %v, want %v", got, want)
	}
}

func TestSplitCommandLine(t *testing.T) {
	cases := map[string][]string{
		`npx -y pkg`:                 {"npx", "-y", "pkg"},
		`  run  "a b"  'c d' e\ f  `: {"run", "a b", "c d", "e f"},
		`x ""`:                       {"x", ""},
	}
	for in, want := range cases {
		got, err := SplitCommandLine(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`a "b`, `a 'b`, ``, `   `} {
		if _, err := SplitCommandLine(bad); err == nil {
			t.Fatalf("%q: no error", bad)
		}
	}
}

func TestSuggestName(t *testing.T) {
	cases := map[string]string{
		"@modelcontextprotocol/server-filesystem": "filesystem",
		"mcp-server-git":                    "git",
		"mcp/postgres:latest":               "postgres",
		"ghcr.io/acme/tools-mcp@sha256:abc": "tools",
		"/usr/local/bin/my_server":          "my_server",
		"https://mcp.example.com/mcp":       "mcp-example-com",
		"!!!":                               "server",
	}
	for in, want := range cases {
		if got := SuggestName(in); got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseInstallURL(t *testing.T) {
	list, problems, err := ParseInstall("https://mcp.example.com/mcp")
	if err != nil || len(problems) > 0 || len(list) != 1 {
		t.Fatalf("got %v %v %v", list, problems, err)
	}
	if list[0].Name != "mcp-example-com" || entryJSON(t, list[0].Entry) != `{"type":"http","url":"https://mcp.example.com/mcp"}` {
		t.Fatalf("got %s %s", list[0].Name, entryJSON(t, list[0].Entry))
	}
	for _, bad := range []string{"http://example.com/mcp", "ftp://x/y", "https://"} {
		if _, _, err := ParseInstall(bad); err == nil {
			t.Fatalf("%q: no error", bad)
		}
	}
	if _, _, err := ParseInstall("http://127.0.0.1:3000/mcp"); err != nil {
		t.Fatalf("loopback http: %v", err)
	}
}

func TestParseInstallJSONMovesSecrets(t *testing.T) {
	tok := "sk-" + "test0123456789abcdef"
	list, _, err := ParseInstall(`{"mcpServers":{"a":{"command":"x","env":{"KEY":"` + tok + `"}}}}`)
	if err != nil || len(list) != 1 {
		t.Fatalf("got %v %v", list, err)
	}
	if strings.Contains(entryJSON(t, list[0].Entry), tok) || list[0].Secrets["a_key"] != tok {
		t.Fatalf("secret not moved: %s %v", entryJSON(t, list[0].Entry), len(list[0].Secrets))
	}
}

func TestParseShareKey(t *testing.T) {
	cases := []struct {
		body, source string
		share        string
		problem      bool
	}{
		{`{"command":"x","share":"user"}`, SourceUser, ShareUser, false},
		{`{"command":"x"}`, SourceUser, "", false},
		{`{"command":"x","share":"all"}`, SourceUser, "", true},
		{`{"url":"https://a.example/mcp","share":"user"}`, SourceUser, "", true},
		{`{"command":"x","share":"user"}`, SourceProject, "", true},
	}
	for _, c := range cases {
		list, _, err := ParseFile([]byte(`{"mcpServers":{"a":`+c.body+`}}`), c.source, "f")
		if err != nil || len(list) != 1 {
			t.Fatal(err)
		}
		s := list[0]
		if (s.Problem != "") != c.problem || (!c.problem && s.Share != c.share) {
			t.Fatalf("%s (%s): share %q problem %q", c.body, c.source, s.Share, s.Problem)
		}
	}
}
