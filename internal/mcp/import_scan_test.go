package mcp

import (
	"path/filepath"
	"strings"
	"testing"
)

// ScanOtherApps finds the servers in other apps' known config files
// (both top keys), moves their tokens out of the entries, and leaves out
// names that are already configured.
func TestScanOtherApps(t *testing.T) {
	home := t.TempDir()
	tok := "ghp_" + "testtokenvalue0123456789"
	writeFile(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers": {
	  "gh": {"url": "https://api.example.com/mcp", "headers": {"Authorization": "Bearer `+tok+`"}},
	  "have": {"command": "x"}}}`)
	writeFile(t, filepath.Join(home, ".config", "Code", "User", "mcp.json"), `{"servers": {"fs": {"command": "npx"}}}`)
	writeFile(t, filepath.Join(home, ".gemini", "settings.json"), `{"theme": "x"}`) // no servers: not listed
	writeFile(t, filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"), `{broken`)
	found := ScanOtherApps(OtherAppFiles(home, "linux", ""), map[string]bool{"have": true})
	var lines []string
	for _, f := range found {
		rel, _ := filepath.Rel(home, f.Path)
		var names []string
		for _, im := range f.Servers {
			names = append(names, im.Name)
		}
		lines = append(lines, filepath.ToSlash(rel)+"="+strings.Join(names, ","))
	}
	want := ".config/Code/User/mcp.json=fs .cursor/mcp.json=gh"
	if strings.Join(lines, " ") != want {
		t.Errorf("found %q, want %q", strings.Join(lines, " "), want)
	}
	for _, f := range found {
		for _, im := range f.Servers {
			if im.Name == "gh" && (len(im.Secrets) != 1 || strings.Contains(PreviewLine(im), tok)) {
				t.Errorf("gh: secrets %d, preview %q", len(im.Secrets), PreviewLine(im))
			}
		}
	}
	// The same name in two files is offered once (the first file).
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {"fs": {"command": "other"}}}`)
	n := 0
	for _, f := range ScanOtherApps(OtherAppFiles(home, "linux", ""), nil) {
		for _, im := range f.Servers {
			if im.Name == "fs" {
				n++
			}
		}
	}
	if n != 1 {
		t.Errorf("fs offered %d times, want once", n)
	}
}

// The preview shows what would run: the command line or the url.
func TestPreviewLine(t *testing.T) {
	got, _, _ := ParseImport(`{"mcpServers": {"a": {"command": "npx", "args": ["-y", "pkg"]}, "b": {"url": "https://x.example/mcp"}}}`)
	tok := "ghp_" + "testtokenvalue0123456789"
	more, _, _ := ParseImport(`{"t": {"command": "srv", "args": ["--token", "` + tok + `"]}}`)
	if p := PreviewLine(more[0]); strings.Contains(p, tok) || p != "srv --token •••" {
		t.Errorf("preview shows a token: %q", p)
	}
	if PreviewLine(got[0]) != "npx -y pkg" || PreviewLine(got[1]) != "https://x.example/mcp" {
		t.Errorf("previews %q %q", PreviewLine(got[0]), PreviewLine(got[1]))
	}
}
