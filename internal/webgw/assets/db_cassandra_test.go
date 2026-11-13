package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (DBX-67): the page browses Cassandra / ScyllaDB. CQL has no OFFSET,
// so a table and a query page by the cursor the answer carries (kept as a
// stack, so Previous works); the tree groups a connection's tables by
// keyspace; and what CQL cannot do (a plan, a schema compare, a
// transaction mode, editing rows) is not offered.
func TestDBCassandraPage(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("src", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	state, tabs, tree, phone := read("state/more.ts"), read("views/desk/DBTabs.tsx"), read("views/desk/DeskDB.tsx"), read("views/more/DBView.tsx")
	must := func(what, src string, parts ...string) {
		t.Helper()
		for _, p := range parts {
			if !strings.Contains(src, p) {
				t.Errorf("%s: missing %q", what, p)
			}
		}
	}
	must("state/more.ts", state, "cursor: str(d.cursor)", "view: x.view === true, group: str(x.group)")
	must("the desktop table tab", tabs, `"db.rows", { conn, table, from, cursor`, "r.cursor")
	must("the desktop query tab", tabs, `"db.query", { conn, sql`, "cursor")
	must("the desktop tree", tree, "x.group", `c.driver === "cassandra"`)
	must("the phone table page", phone, `"db.rows", { conn, table, from, cursor`, "data.cursor")
}
