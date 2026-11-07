package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Databases: Open CSV/TSV as Table… is in the palette, never run by the AI,
// and lists the workspace's .csv and .tsv files.
func TestDBOpenCSVPaletteAndFiles(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-open-csv"
	}
	if !found {
		t.Fatal("db-open-csv not in the palette")
	}
	if _, ok := aiActionPolicies["db-open-csv"]; !ok {
		t.Fatal("db-open-csv has no AI policy")
	}
	root := t.TempDir()
	for _, f := range []string{"a.csv", "b.TSV", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(dbFindCSVOrTSV(root), ",")
	if got != "a.csv,b.TSV" {
		t.Fatalf("files = %q", got)
	}
}

// Loading a CSV adds a session-only SQLite connection (never saved to a
// config file) that survives a reload, and queues SELECT * in its console.
func TestDBOpenCSVLoadsConnection(t *testing.T) {
	root := t.TempDir()
	csv := filepath.Join(root, "Sales 2026.csv")
	if err := os.WriteFile(csv, []byte("id,name\n1,ann\n2,bob\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Model{w: 120, h: 40}
	m.databases = newDBState()
	m.databases.loaded = true
	m.databases.root = root
	msg := dbOpenCSVLoad(csv, t.TempDir())()
	done, ok := msg.(dbOpenCSVMsg)
	if !ok || done.err != nil {
		t.Fatalf("load: %#v", msg)
	}
	if done.tbl.Table != "sales_2026" || done.tbl.Rows != 2 {
		t.Fatalf("table = %+v", done.tbl)
	}
	c := m.databaseAddCSVConn(done)
	if c.Driver != "sqlite" || !strings.HasPrefix(c.ID, "csv-") {
		t.Fatalf("conn = %+v", c)
	}
	if _, err := os.Stat(filepath.Join(root, ".termocode", "databases.json")); err == nil {
		t.Fatal("the CSV connection was saved to the project config")
	}
	m.databases.load(root)
	if _, ok := m.databases.connection(c.ID); !ok {
		t.Fatal("the CSV connection is lost after a reload")
	}
	if got := dbOpenCSVSelect(done.tbl); got != `SELECT * FROM "sales_2026";` {
		t.Fatalf("select = %q", got)
	}
	// Loading the same file again replaces the connection, not a second one.
	m.databaseAddCSVConn(done)
	n := 0
	for _, cc := range m.databases.conns {
		if cc.ID == c.ID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d copies of the CSV connection", n)
	}
	_ = dbclient.Conn{}
}
