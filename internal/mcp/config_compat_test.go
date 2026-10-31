package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pasted configs from other apps use ${env:NAME}, ${workspaceFolderBasename}
// and ${/}; they resolve like termocode's own ${NAME} and ${workspaceFolder}.
func TestExpandOtherAppsVariables(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "my-proj")
	env := map[string]string{"TOKEN": "abc"}
	x := Expander{
		Env:       func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Workspace: ws,
		Home:      "/home/u",
	}
	s := Server{Name: "a", Command: "run", Args: []string{
		"${env:TOKEN}", "${workspaceFolderBasename}", "a${/}b", "${env:MISSING:-dflt}",
	}}
	got, err := s.Expand(x)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"abc", "my-proj", "a" + string(os.PathSeparator) + "b", "dflt"}
	for i := range want {
		if got.Args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got.Args[i], want[i])
		}
	}
	_, err = Server{Name: "b", Command: "${env:NOPE}"}.Expand(x)
	var ne *NeedsError
	if !errors.As(err, &ne) || ne.What != "${NOPE}" {
		t.Errorf("missing ${env:NOPE}: err = %v, want needs ${NOPE}", err)
	}
}

// The top-level "servers" key (other editors' shape) is read like
// "mcpServers"; on a name clash the mcpServers entry wins.
func TestParseServersKeyAlias(t *testing.T) {
	body := `{
	  "servers": {"fs": {"command": "npx"}, "dup": {"command": "from-servers"}},
	  "mcpServers": {"dup": {"command": "from-mcpServers"}}
	}`
	got, _, err := ParseFile([]byte(body), SourceUser, "/u/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Server{}
	for _, s := range got {
		by[s.Name] = s
	}
	if len(got) != 2 || by["fs"].Command != "npx" || by["dup"].Command != "from-mcpServers" {
		t.Errorf("got %+v", got)
	}
}

// A file written with only the "servers" key is edited in place:
// Disable / Enable / Remove change that key and add no mcpServers block.
func TestEditServersKeyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	writeFile(t, p, `{"servers": {"fs": {"command": "npx"}, "gone": {"command": "x"}}}`)
	if err := SetDisabled(p, "fs", true); err != nil {
		t.Fatal(err)
	}
	if err := RemoveServer(p, "gone"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	list, _, err := ParseFile(b, SourceUser, p)
	if err != nil || len(list) != 1 || !list[0].Disabled || list[0].Command != "npx" {
		t.Fatalf("after edits: %s → %+v (%v)", b, list, err)
	}
	if err := SetDisabled(p, "fs", false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "mcpServers") {
		t.Errorf("edit added an mcpServers block: %s", b)
	}
}

// "Paste a config…" takes a "servers" block too.
func TestParseImportServersKey(t *testing.T) {
	got, problems, err := ParseImport(`{"servers": {"fs": {"command": "npx"}}}`)
	if err != nil || len(problems) != 0 || len(got) != 1 || got[0].Name != "fs" {
		t.Errorf("got %+v %v %v", got, problems, err)
	}
}
