package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbTestConn(id string) dbclient.Conn { return dbclient.Conn{ID: id, Driver: "postgres"} }

// DBX-6: the new commands are in the palette with an AI policy.
func TestDBSavedQueriesPaletteAndPolicy(t *testing.T) {
	want := map[string]string{
		"db-save-query":         "Databases: Save Query…",
		"db-saved-queries":      "Databases: Saved Queries…",
		"db-history-connection": "Databases: History of This Connection…",
		"db-another-console":    "Databases: Another Console",
	}
	for _, it := range databasesPaletteItems() {
		if title, ok := want[it.ID]; ok && title == it.Title {
			delete(want, it.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing palette items: %v", want)
	}
	for _, id := range []string{"db-save-query", "db-saved-queries", "db-history-connection", "db-another-console"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("%s has no AI policy", id)
		}
	}
}

// DBX-6: several consoles per connection: the second one is a new file
// next to the first, and its tab says which one it is.
func TestDBConsolePathNumbered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := &Model{}
	first := m.databaseConsolePathN("local", 1)
	second := m.databaseConsolePathN("local", 2)
	if first == "" || first != m.databaseConsolePath("local") || second == first || !strings.HasSuffix(second, "-2.sql") {
		t.Fatalf("paths: %q %q", first, second)
	}
	m.databases = &dbState{}
	m.databases.conns = append(m.databases.conns, dbTestConn("local"))
	if segs, ok := m.databaseConsoleCrumbs(second); !ok || len(segs) != 2 || segs[0] != "SQL console 2" {
		t.Fatalf("crumbs: %v %v", segs, ok)
	}
}

// DBX-6: live templates reach the omnifunc payload.
func TestDBSchemaPayloadHasTemplates(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{}}
	p := s.schemaPayload(dbTestConn("local"))
	tpl, ok := p["templates"].([]map[string]any)
	if !ok || len(tpl) == 0 {
		t.Fatalf("templates: %v", p["templates"])
	}
	found := false
	for _, x := range tpl {
		if x["a"] == "sel" && x["t"] == "SELECT * FROM ;" && x["c"] == 14 {
			found = true
		}
	}
	if !found {
		t.Fatalf("sel template: %v", tpl)
	}
}
