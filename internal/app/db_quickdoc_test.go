package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbQuickDocTestState() *dbState {
	s := newDBState()
	t := "pg/db/shop/schema/sales/Tables/orders"
	s.nodes["pg/db/shop/schema/sales/Tables"] = dbNode{kind: dbNodeGroup, name: "Tables", conn: "pg", children: []string{t}}
	s.nodes[t] = dbNode{kind: dbNodeTable, name: "orders", path: t, conn: "pg", loaded: true,
		children: []string{t + "/col/id", t + "/col/total", t + "/fk/f1"}}
	s.nodes[t+"/col/id"] = dbNode{kind: dbNodeColumn, name: "id  bigint", path: t + "/col/id", conn: "pg", detail: "PK"}
	s.nodes[t+"/col/total"] = dbNode{kind: dbNodeColumn, name: "total  numeric", path: t + "/col/total", conn: "pg"}
	s.nodes[t+"/fk/f1"] = dbNode{kind: dbNodeDetail, name: "Foreign key · id → users.id", path: t + "/fk/f1", conn: "pg"}
	r := "pg/db/shop/schema/sales/Routines/total_of"
	s.nodes[r] = dbNode{kind: dbNodeRoutine, name: "total_of", path: r, conn: "pg"}
	return s
}

func TestDBQuickDocPaletteItems(t *testing.T) {
	want := map[string]string{"db-quick-doc": "Databases: Quick Documentation", "db-insert-hints": "Databases: Show INSERT Column Hints"}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Fatalf("%s title %q", it.ID, it.Title)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing palette items %v", want)
	}
}

func TestDBQuickDocTable(t *testing.T) {
	s := dbQuickDocTestState()
	title, lines := dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "table", Table: "orders"})
	text := strings.Join(lines, "\n")
	if title != "Table sales.orders" {
		t.Fatalf("title %q", title)
	}
	for _, w := range []string{"id  bigint  PK", "total  numeric", "Foreign key · id → users.id"} {
		if !strings.Contains(text, w) {
			t.Errorf("no %q in\n%s", w, text)
		}
	}
}

func TestDBQuickDocColumnAndRoutine(t *testing.T) {
	s := dbQuickDocTestState()
	title, lines := dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "column", Table: "orders", Column: "total"})
	if title != "Column sales.orders.total" || !strings.Contains(strings.Join(lines, "\n"), "Type: numeric") {
		t.Fatalf("column doc %q %v", title, lines)
	}
	// A bare column with no table: every loaded table that has it.
	_, lines = dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "column", Column: "id"})
	if !strings.Contains(strings.Join(lines, "\n"), "sales.orders") {
		t.Fatalf("bare column doc %v", lines)
	}
	title, _ = dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "routine", Routine: "total_of"})
	if title != "Routine sales.total_of" {
		t.Fatalf("routine title %q", title)
	}
	title, lines = dbQuickDoc(s, "pg", dbclient.SQLRef{Kind: "table", Table: "nope"})
	if title != "Table nope" || !strings.Contains(strings.Join(lines, "\n"), "Not in the Databases tree") {
		t.Fatalf("unknown table %q %v", title, lines)
	}
}

func TestDBQuickDocTreeNode(t *testing.T) {
	s := dbQuickDocTestState()
	title, lines := dbQuickDocNode(s, "pg/db/shop/schema/sales/Tables/orders/col/id")
	if title != "Column sales.orders.id" || !strings.Contains(strings.Join(lines, "\n"), "Primary key") {
		t.Fatalf("tree column doc %q %v", title, lines)
	}
}

func TestDBOffsetAndHintPositions(t *testing.T) {
	lines := []string{"INSERT INTO t (a, b)", "VALUES (1, 'x');"}
	if got := dbOffsetAt(lines, 1, 8); got != len(lines[0])+1+8 {
		t.Fatalf("offset %d", got)
	}
	hints := dbclient.InsertValueHints(strings.Join(lines, "\n"), "postgres")
	pos := dbHintPositions(lines, hints)
	if len(pos) != 2 || pos[0] != (dbHintPos{Row: 1, Col: 8, Text: "a:"}) || pos[1] != (dbHintPos{Row: 1, Col: 11, Text: "b:"}) {
		t.Fatalf("positions %+v", pos)
	}
}
