package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

func fakeCatalogServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servers":[
 {"server":{"name":"io.github.acme/notes","description":"Read and write notes.","version":"1.2.0",
   "packages":[{"registryType":"npm","identifier":"@acme/notes-mcp","version":"1.2.0","transport":{"type":"stdio"},
     "environmentVariables":[{"name":"NOTES_TOKEN","isSecret":true}]}]}},
 {"server":{"name":"com.example/old","description":"Old one.","version":"0.1.0",
   "packages":[{"registryType":"pypi","identifier":"old-mcp","version":"0.1.0"}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"deprecated"}}}
],"metadata":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// MCPX-16: AI: MCP: Browse Catalog… uses the network only on "Load";
// the list is cached; picking a server opens a Cancel-first card with
// the exact entry; Add writes it to the user's mcp.json.
func TestMCPCatalogLoadPickAndAdd(t *testing.T) {
	var hits atomic.Int32
	srv := fakeCatalogServer(t, &hits)
	m := covMCPModel(t)
	if err := mcp.SaveSetting(mcpConfigPath(), "mcp_catalog_url", srv.URL); err != nil {
		t.Fatal(err)
	}
	if c, ok := m.dispatchAIPalette("ai-mcp-catalog"); !ok || c != nil || !m.pickerOpen || m.pickerKind != pickerKindMCPCatalog {
		t.Fatal("the palette entry did not open the catalog")
	}
	if hits.Load() != 0 {
		t.Fatal("opening the catalog used the network")
	}
	if !strings.Contains(m.picker.View(), "Load the catalog") {
		t.Fatalf("no Load row:\n%s", m.picker.View())
	}
	cmd, _ := m.handleMCPPickerSelect(mcpCatalogLoadID)
	msg, ok := covCollect[mcpCatalogLoadedMsg](cmd)
	if !ok || msg.err != nil || hits.Load() != 1 {
		t.Fatalf("load: %+v hits=%d", msg, hits.Load())
	}
	m.onMCPCatalogLoaded(msg)
	view := m.picker.View()
	for _, want := range []string{"notes", "github.com/acme", "deprecated"} {
		if !strings.Contains(view, want) {
			t.Errorf("catalog lacks %q:\n%s", want, view)
		}
	}
	// A second open shows the cache, no network.
	m.pickerOpen = false
	m.openMCPCatalog()
	if !strings.Contains(m.picker.View(), "notes") || hits.Load() != 1 {
		t.Fatal("the cached list was not shown")
	}
	m.handleMCPPickerSelect("io.github.acme/notes")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPCatalogAdd {
		t.Fatal("no install card")
	}
	card := m.confirm.View()
	for _, want := range []string{"npx", "@acme/notes-mcp@1.2.0", "notes_notes_token", "not verified"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	if m.answerMCPCatalogAdd("cancel"); fileHas(mcpUserPath(), "notes") {
		t.Fatal("Cancel added the server")
	}
	m.handleMCPPickerSelect("io.github.acme/notes")
	m.answerMCPCatalogAdd("add")
	if !fileHas(mcpUserPath(), `"@acme/notes-mcp@1.2.0"`) {
		t.Fatal("Add did not write the entry")
	}
}

func fileHas(p, s string) bool {
	b, _ := os.ReadFile(p)
	return strings.Contains(string(b), s)
}

func TestMCPCatalogURLSettingRow(t *testing.T) {
	m := aiTestModel(t)
	if v, ok := mcpSettingValue("setting-mcp-catalog-url"); !ok || v != "" {
		t.Fatalf("row %q %v", v, ok)
	}
	m.applySetting("setting-mcp-catalog-url", "https://mcp.corp.example")
	if s := mcp.LoadSettings(mcpConfigPath()); s.CatalogURL != "https://mcp.corp.example" {
		t.Fatalf("saved %q", s.CatalogURL)
	}
	before, _ := os.ReadFile(mcpConfigPath())
	m.applySetting("setting-mcp-catalog-url", "http://evil.example")
	if after, _ := os.ReadFile(mcpConfigPath()); string(after) != string(before) {
		t.Fatal("a plain http URL was saved")
	}
	m.applySetting("setting-mcp-catalog-url", "")
	if s := mcp.LoadSettings(mcpConfigPath()); s.CatalogURL != "" {
		t.Fatal("empty did not go back to the official registry")
	}
	if p, ok := aiSettingPolicies["mcp_catalog_url"]; !ok || p.Gate != aiGateNever {
		t.Fatal("the AI must never change mcp_catalog_url")
	}
}
