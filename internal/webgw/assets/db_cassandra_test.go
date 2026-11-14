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
	must("the desktop table tab", tabs, `"db.rows", { conn, table, ...pages[`, "r.cursor")
	// NEW-7: the console reads the rest of a CQL result by its cursor (Next rows).
	must("the desktop query tab", tabs, `"db.query", { conn, sql`, "cursor: view.r.cursor")
	must("the desktop tree", tree, "x.group", `c.driver === "cassandra"`)
	must("the phone table list (a heading per keyspace)", phone, "x.group !== ", `class="db-group"`)
	must("the phone table page", phone, `"db.rows", { conn, table, from, cursor`, "data.cursor")
}

// Spec (NEW-7): to make room in workbench.js for the console's paging,
// a table's pending edits turn into db.edit's args in aictx.js (loaded
// on first use, like the grid's Export menu); workbench.js keeps only
// the call.
func TestDBEditRequestInLazyChunk(t *testing.T) {
	wb, ai := readBuilt(t, "workbench.js"), readBuilt(t, "aictx.js")
	if !strings.Contains(ai, `kind:"insert"`) || !strings.Contains(ai, `kind:"update"`) || !strings.Contains(ai, `kind:"delete"`) {
		t.Error("aictx.js does not build db.edit's edits")
	}
	if strings.Contains(wb, `kind:"insert"`) {
		t.Error("workbench.js still builds db.edit's edits; they belong in aictx.js")
	}
	if !strings.Contains(wb, `"db.edit"`) {
		t.Error("workbench.js does not send db.edit")
	}
}
