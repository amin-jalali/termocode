package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// fakeVersionsCatalog serves notes at version v (status st).
func fakeVersionsCatalog(t *testing.T, v, st *atomic.Value, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		ver := v.Load().(string)
		body, _ := json.Marshal(map[string]any{"servers": []any{map[string]any{
			"server": map[string]any{"name": "io.github.acme/notes", "version": ver,
				"packages": []any{map[string]any{"registryType": "npm", "identifier": "@acme/notes-mcp", "version": ver}}},
			"_meta": map[string]any{"io.modelcontextprotocol.registry/official": map[string]any{"status": st.Load().(string)}},
		}}})
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// MCPX-32: a catalog server is pinned; Check for Updates (network on
// request) tells about a newer version and a deprecated flag; the list
// and the server actions show it; Update asks first (Cancel first) and
// moves the pin.
func TestMCPUpdatesNoticeAndUpdate(t *testing.T) {
	var v, st atomic.Value
	v.Store("1.2.0")
	st.Store("active")
	var hits atomic.Int32
	srv := fakeVersionsCatalog(t, &v, &st, &hits)
	m := covMCPModel(t)
	_ = mcp.SaveSetting(mcpConfigPath(), "mcp_catalog_url", srv.URL)
	cat := mcpCatalog()
	list, err := cat.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, entry, _ := list[0].ServerEntry()
	if err := mcp.AddServer(mcpUserPath(), "notes", entry); err != nil {
		t.Fatal(err)
	}
	v.Store("1.3.0")
	st.Store("deprecated")
	if c, ok := m.dispatchAIPalette("ai-mcp-updates"); !ok || c == nil {
		t.Fatal("no update check")
	} else {
		msg, ok := covCollect[mcpUpdatesMsg](c)
		if !ok || msg.err != nil {
			t.Fatalf("check: %+v", msg)
		}
		m.onMCPUpdates(msg)
	}
	if !strings.Contains(covToastText(m), "notes 1.3.0") {
		t.Errorf("toast = %q", covToastText(m))
	}
	m.openMCPServers()
	if view := m.picker.View(); !strings.Contains(view, "1.3.0") {
		t.Errorf("servers list lacks the update:\n%s", view)
	}
	items := mcpServerActionItems(m, "notes")
	var found bool
	for _, it := range items {
		if it.ID == "update" && strings.Contains(it.Title, "1.3.0") && strings.Contains(it.Hint, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Update item: %+v", items)
	}
	m.aiS().mcpSel = "notes"
	m.onMCPServerAction("update")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPUpdate || !strings.Contains(m.confirm.View(), "@acme/notes-mcp@1.3.0") {
		t.Fatal("no update card")
	}
	m.answerMCPUpdate("cancel")
	if b, _ := os.ReadFile(mcpUserPath()); strings.Contains(string(b), "1.3.0") {
		t.Fatal("Cancel updated")
	}
	m.aiS().mcpSel = "notes"
	m.onMCPServerAction("update")
	m.answerMCPUpdate("update")
	if b, _ := os.ReadFile(mcpUserPath()); !strings.Contains(string(b), "@acme/notes-mcp@1.3.0") {
		t.Fatalf("not updated: %s", b)
	}
}

// mcp_auto_update off: opening the servers list never uses the network.
func TestMCPAutoUpdateOffNoNetwork(t *testing.T) {
	var v, st atomic.Value
	v.Store("1.2.0")
	st.Store("active")
	var hits atomic.Int32
	srv := fakeVersionsCatalog(t, &v, &st, &hits)
	m := covMCPModel(t)
	_ = mcp.SaveSetting(mcpConfigPath(), "mcp_catalog_url", srv.URL)
	if c := m.openMCPServers(); c != nil {
		t.Fatal("off: the list started a check")
	}
	_ = mcp.SaveSetting(mcpConfigPath(), "mcp_auto_update", "notify")
	if c := m.openMCPServers(); c == nil {
		t.Fatal("notify with no recent check: no check started")
	}
	if hits.Load() != 0 {
		t.Fatal("the network was used before the command ran")
	}
}
