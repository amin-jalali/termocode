package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/charmbracelet/x/ansi"
)

// Spec (MCPX-7): AI: MCP: Import from Other Apps… shows a preview card
// (Cancel first and focused) of the servers found in other apps' files;
// Import adds them to mcp.json with their tokens in the secret store.
// The card names files, never apps.
func TestMCPImportOtherApps(t *testing.T) {
	m := aiTestModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	tok := "ghp_" + "testtokenvalue0123456789"
	src := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"mcpServers": {"gh": {"url": "https://api.example.com/mcp", "headers": {"Authorization": "Bearer ` + tok + `"}},
	  "fs": {"command": "npx", "args": ["-y", "server-fs"]}}}`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openMCPImportOther()
	checkCardRule(t, "import", m, false)
	v := ansi.Strip(m.confirm.View())
	for _, want := range []string{"gh", "https://api.example.com/mcp", "npx -y server-fs", "~/.cursor/mcp.json"} {
		if !strings.Contains(v, want) {
			t.Errorf("card misses %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, tok) {
		t.Error("the card shows a token")
	}
	// Cancel (and Esc) change nothing.
	m.answerMCPImportOther("cancel")
	if len(mcpServerEntries("gh")) != 0 {
		t.Fatal("Cancel imported")
	}
	m.openMCPImportOther()
	m.answerMCPImportOther("import")
	if len(mcpServerEntries("gh")) != 1 || len(mcpServerEntries("fs")) != 1 {
		t.Fatal("Import did not add the servers")
	}
	b, _ := os.ReadFile(mcpUserPath())
	if strings.Contains(string(b), tok) {
		t.Error("mcp.json holds the token")
	}
	cr, _ := ai.LoadCredentials(mcpCredPath())
	found := false
	for _, v := range cr.MCPSecrets {
		found = found || v == tok
	}
	if !found {
		t.Error("the token is not in the secret store")
	}
	// Nothing new: a toast, no card.
	m.confirmOpen = false
	m.openMCPImportOther()
	if m.confirmOpen {
		t.Error("a card with nothing to import")
	}
}
