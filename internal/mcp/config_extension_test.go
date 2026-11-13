package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

// DI-20: an extension folder can carry an mcp.json; its servers load below
// the user's and the project's files, and count as project servers: they
// run a command, so they are never trusted by themselves.
func TestLoadAllReadsServersOfAnExtension(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	writeFile(t, filepath.Join(dir, "extensions", "notes-pack", "mcp.json"),
		`{"mcpServers":{"notes":{"command":"notes-mcp"},"shared":{"command":"from-extension"}}}`)
	writeFile(t, user, `{"mcpServers":{"shared":{"command":"from-user"}}}`)
	got, warns := LoadAll(user, "")
	if len(warns) != 0 {
		t.Fatalf("warnings %v", warns)
	}
	by := map[string]Server{}
	for _, s := range got {
		by[s.Name] = s
	}
	n, ok := by["notes"]
	if !ok {
		t.Fatalf("the extension's server is missing: %+v", got)
	}
	if n.Source != SourceProject || !strings.HasSuffix(filepath.ToSlash(n.SourcePath), "extensions/notes-pack/mcp.json") {
		t.Errorf("source %q path %q: an extension server must count as a project server", n.Source, n.SourcePath)
	}
	if by["shared"].Command != "from-user" {
		t.Errorf("the user's file must win over an extension: %+v", by["shared"])
	}
}
