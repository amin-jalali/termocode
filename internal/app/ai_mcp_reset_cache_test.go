package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (RT-14): "AI: MCP: Reset Tool Lists" forgets every saved tool
// list and says how many; the next use lists again.
func TestMCPResetToolListsPalette(t *testing.T) {
	m := aiTestModel(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	dir := filepath.Join(cache, "termocode", "mcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeUserMCP(t, `{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}}}`)
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n+".tools.json"), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := m.dispatchAIPalette("ai-mcp-reset-cache"); !ok {
		t.Fatal("not dispatched")
	}
	if txt := toastText(m); !strings.Contains(txt, "2 servers") {
		t.Fatalf("toast %q", txt)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("left %d files", len(left))
	}
	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-mcp-reset-cache" {
			found = true
		}
	}
	if !found {
		t.Fatal("no palette entry")
	}
}
