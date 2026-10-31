package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

// The common project file <root>/.mcp.json is read as a project source
// (the same trust card): it replaces the user's entry of a name, and
// .termocode/mcp.json wins over it on a clash.
func TestLoadCommonProjectFile(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user", "mcp.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, user, `{"mcpServers": {"a": {"command": "user-a"}, "b": {"command": "user-b"}}}`)
	writeFile(t, filepath.Join(root, ".mcp.json"), `{"mcpServers": {"b": {"command": "common-b"}, "c": {"command": "common-c"}, "d": {"command": "common-d"}}}`)
	writeFile(t, ProjectPath(root), `{"mcpServers": {"d": {"command": "own-d"}}}`)
	got, warns := LoadAll(user, ProjectPath(root))
	if len(warns) != 0 {
		t.Errorf("warnings %q", warns)
	}
	var lines []string
	for _, s := range got {
		lines = append(lines, s.Name+"="+s.Command+"/"+s.Source+"/"+filepath.Base(s.SourcePath))
	}
	want := "a=user-a/user/mcp.json b=common-b/project/.mcp.json c=common-c/project/.mcp.json d=own-d/project/mcp.json"
	if strings.Join(lines, " ") != want {
		t.Errorf("merged %q\nwant %q", strings.Join(lines, " "), want)
	}
	if l := ProjectFileLabel(filepath.Join(root, ".mcp.json")); l != ".mcp.json" {
		t.Errorf("label = %q", l)
	}
	if l := ProjectFileLabel(ProjectPath(root)); l != ".termocode/mcp.json" {
		t.Errorf("label = %q", l)
	}
}

// .mcp.json is untrusted like .termocode/mcp.json: ${cmd:} and auth.gh
// are refused there.
func TestCommonProjectFileIsUntrusted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".mcp.json"), `{"mcpServers": {
		"c": {"command": "x", "env": {"T": "${cmd:cat ~/.token}"}},
		"g": {"url": "https://api.example.com/mcp", "auth": {"gh": "me"}}}}`)
	got, _ := LoadAll("", ProjectPath(root))
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for _, s := range got {
		if s.Source != SourceProject || !strings.HasPrefix(s.Problem, "invalid:") {
			t.Errorf("%s: source %q problem %q, want project + invalid", s.Name, s.Source, s.Problem)
		}
	}
}
