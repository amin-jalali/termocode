package httpclient

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// `# @prompt name [label]` lines are listed in order and kept out of
// Directives.
func TestParsePromptDirectives(t *testing.T) {
	f := Parse("# @prompt user\n# @prompt password Your password\nGET http://x/{{user}}\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests = %d", len(f.Requests))
	}
	r := f.Requests[0]
	want := []Prompt{{Name: "user"}, {Name: "password", Label: "Your password"}}
	if !slices.Equal(r.Prompts, want) {
		t.Fatalf("prompts = %+v", r.Prompts)
	}
	if _, ok := r.Directives["prompt"]; ok {
		t.Error("prompt left in Directives")
	}
	if !r.Prompts[1].Secret() || r.Prompts[0].Secret() {
		t.Error("Secret() wrong")
	}
}

// Order: prompted values, file variables, the environment, folder
// variables (nearest http-client.vars.json wins), then globals. A
// prompted secret is reported to OnSecret.
func TestResolverPromptFolderGlobalOrder(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, FolderVarsFile), `{"a":"root","f":"rootf","n":7}`)
	write(filepath.Join(sub, FolderVarsFile), `{"f":"subf"}`)
	write(filepath.Join(root, EnvFile), `{"dev":{"e":"env","a":"enva"}}`)
	var secrets []string
	rv := &Resolver{
		FileVars: []Var{{Name: "p", Value: "file"}},
		Env:      "dev",
		Envs:     LoadEnvs(sub, root),
		Globals:  map[string]string{"g": "glob", "f": "globf", "a": "globa"},
		Prompted: map[string]string{"p": "typed", "password": "hunter22"},
		OnSecret: func(s string) { secrets = append(secrets, s) },
	}
	rr, un := rv.Resolve(Request{Method: "GET", URL: "http://x/{{p}}/{{e}}/{{a}}/{{f}}/{{n}}/{{g}}/{{password}}"})
	if len(un) != 0 {
		t.Fatalf("unresolved %+v", un)
	}
	if rr.URL != "http://x/typed/env/enva/subf/7/glob/hunter22" {
		t.Fatalf("url = %s", rr.URL)
	}
	if !slices.Contains(rr.Secrets, "hunter22") || !slices.Contains(secrets, "hunter22") {
		t.Errorf("prompted password not secret: %v %v", rr.Secrets, secrets)
	}
	if !rv.Known("g") || !rv.Known("f") || rv.Known("zz") {
		t.Error("Known wrong")
	}
	if names := rv.Envs.FolderVarNames(); !slices.Equal(names, []string{"a", "f", "n"}) {
		t.Errorf("folder names = %v", names)
	}
}
