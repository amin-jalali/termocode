package app

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func writeTestBundle(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "notes.mcpb")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"manifest.json": `{"manifest_version":"0.2","name":"notes-box","display_name":"Notes Box","version":"1.4.0",
 "description":"Your notes as tools.","author":{"name":"Acme"},
 "server":{"type":"node","mcp_config":{"command":"node","args":["${__dirname}/index.js","--dir=${user_config.dir}"],
   "env":{"NOTES_KEY":"${user_config.api_key}"}}},
 "user_config":{"api_key":{"type":"string","title":"API key","sensitive":true,"required":true},
   "dir":{"type":"directory","title":"Notes folder","default":"/tmp/notes"}},
 "tools":[{"name":"find_note"}]}`,
		"index.js": "//",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
	return p
}

// MCPX-31: Install Bundle asks the path, shows the manifest on a
// Cancel-first card, asks user_config (the secret masked), then a
// Cancel-first install card; Install unpacks, saves the secret in the
// store and adds the entry.
func TestMCPBundleInstallFlow(t *testing.T) {
	m := covMCPModel(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	bundle := writeTestBundle(t)
	if c, ok := m.dispatchAIPalette("ai-mcp-bundle"); !ok || c != nil || !m.promptOpen || m.promptKind != promptKindMCPBundlePath {
		t.Fatal("the palette entry did not ask for the file")
	}
	m.handleMCPPromptSubmit(bundle)
	if !m.confirmOpen || m.confirmKind != confirmKindMCPBundle {
		t.Fatal("no manifest card")
	}
	card := m.confirm.View()
	for _, want := range []string{"Notes Box", "1.4.0", "Acme", "find_note", "API key"} {
		if !strings.Contains(card, want) {
			t.Errorf("manifest card lacks %q:\n%s", want, card)
		}
	}
	m.confirmOpen = false
	m.answerMCPBundle("next")
	// api_key first (sorted): masked.
	if !m.promptOpen || m.promptKind != promptKindMCPBundleSecret {
		t.Fatal("no field prompt")
	}
	fake := "sk-" + "test-bundle-123456"
	m.handleMCPPromptSubmit(fake)
	m.handleMCPPromptSubmit("/srv/notes")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPBundle {
		t.Fatal("no install card")
	}
	if card := m.confirm.View(); strings.Contains(card, fake) || !strings.Contains(card, "--dir=/srv/notes") {
		t.Fatalf("install card:\n%s", card)
	}
	if b, _ := os.ReadFile(mcpCredPath()); strings.Contains(string(b), fake) {
		t.Fatal("the secret was saved before Install")
	}
	m.confirmOpen = false
	m.answerMCPBundle("install")
	b, _ := os.ReadFile(mcpUserPath())
	if !strings.Contains(string(b), `"notes-box"`) || !strings.Contains(string(b), "--dir=/srv/notes") || strings.Contains(string(b), fake) {
		t.Fatalf("mcp.json = %s", b)
	}
	c, _ := ai.LoadCredentials(mcpCredPath())
	if c.MCPSecrets["notes_box_api_key"] != fake {
		t.Fatal("the secret is not in the store")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_DATA_HOME"), "termocode", "mcp-bundles", "notes-box-1.4.0", "index.js")); err != nil {
		t.Fatalf("not unpacked: %v", err)
	}
}

// Cancel on the manifest card stops everything.
func TestMCPBundleCancel(t *testing.T) {
	m := covMCPModel(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m.dispatchAIPalette("ai-mcp-bundle")
	m.handleMCPPromptSubmit(writeTestBundle(t))
	m.promptOpen = false // the app closes a prompt on submit
	m.confirmOpen = false
	m.answerMCPBundle("cancel")
	if m.promptOpen || m.aiS().mcpBundle != nil {
		t.Fatal("Cancel went on")
	}
	if b, _ := os.ReadFile(mcpUserPath()); strings.Contains(string(b), "notes-box") {
		t.Fatal("Cancel added the server")
	}
}
