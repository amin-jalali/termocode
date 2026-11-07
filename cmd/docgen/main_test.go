package main

import (
	"os"
	"strings"
	"testing"
)

// TestDocsUpToDate fails when docs/reference/*.md no longer match the
// code (a key, palette command, setting or config file changed).
func TestDocsUpToDate(t *testing.T) {
	stale, err := run("../..", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("reference docs are out of date: %s\nRun `go run ./cmd/docgen` from the repository root and commit the result.",
			strings.Join(stale, ", "))
	}
}

// TestRenderDeterministic guards against map-order output.
func TestRenderDeterministic(t *testing.T) {
	a, b := render(), render()
	for name := range a {
		if a[name] != b[name] {
			t.Fatalf("%s differs between two renders", name)
		}
		if !strings.HasPrefix(a[name], banner) {
			t.Fatalf("%s does not start with the generated banner", name)
		}
	}
}

// TestSettingsMDMCPServerInMCPGroup: mcp_server is a Settings UI row in
// the MCP group (AH-9), so the committed settings.md lists it there and
// no longer under "AI (file only)".
func TestSettingsMDMCPServerInMCPGroup(t *testing.T) {
	raw, err := os.ReadFile("../../docs/reference/settings.md")
	if err != nil {
		t.Fatal(err)
	}
	section := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "## ") {
			section = strings.TrimPrefix(l, "## ")
		}
		if strings.HasPrefix(l, "| `mcp_server` |") {
			if section != "MCP" {
				t.Fatalf("mcp_server is under %q, want MCP", section)
			}
			if !strings.Contains(l, "Applied at once.") {
				t.Fatalf("mcp_server row should say it applies at once: %s", l)
			}
			return
		}
	}
	t.Fatal("settings.md has no mcp_server row")
}
