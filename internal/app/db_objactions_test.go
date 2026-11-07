package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBObjectActionsPaletteItem(t *testing.T) {
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-object-actions" {
			if it.Title != "Databases: Object Actions…" {
				t.Fatalf("title %q", it.Title)
			}
			return
		}
	}
	t.Fatal("no db-object-actions palette item")
}

func dbActionIDs(kind dbNodeKind, family string) string {
	var ids []string
	for _, it := range dbObjectActionItems(kind, family) {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

func TestDBObjectActionItemsPerNode(t *testing.T) {
	cases := []struct {
		kind   dbNodeKind
		family string
		want   string
	}{
		{dbNodeTable, "postgres", "copy,truncate,drop,drop-cascade"},
		{dbNodeTable, "sqlite", "copy,truncate,drop"},
		{dbNodeView, "mysql", "drop"},
		{dbNodeColumn, "postgres", "make-pk"},
		{dbNodeColumn, "sqlite", ""},
		{dbNodeSchema, "postgres", "create-schema,drop"},
		{dbNodeDatabase, "mysql", "create-schema,drop"},
		{dbNodeConnection, "postgres", "create-database"},
		{dbNodeConnection, "sqlite", ""},
	}
	for _, c := range cases {
		if got := dbActionIDs(c.kind, c.family); got != c.want {
			t.Errorf("%v %s = %q, want %q", c.kind, c.family, got, c.want)
		}
	}
}

func TestDBObjectActionFromRow(t *testing.T) {
	tg := dbObjTarget{kind: dbNodeTable, schema: "sales", name: "orders"}
	a := dbObjectActionFor("drop-cascade", tg, "")
	if a.Kind != dbclient.ActDrop || a.Object != "table" || !a.Cascade || a.Schema != "sales" || a.Name != "orders" {
		t.Fatalf("drop cascade %+v", a)
	}
	a = dbObjectActionFor("copy", tg, "orders_bak")
	if a.Kind != dbclient.ActCopyTable || a.NewName != "orders_bak" {
		t.Fatalf("copy %+v", a)
	}
	col := dbObjTarget{kind: dbNodeColumn, schema: "sales", name: "orders", column: "id"}
	a = dbObjectActionFor("make-pk", col, "")
	if a.Kind != dbclient.ActMakePK || a.Name != "orders" || len(a.Columns) != 1 || a.Columns[0] != "id" {
		t.Fatalf("make pk %+v", a)
	}
	sch := dbObjTarget{kind: dbNodeSchema, name: "sales"}
	if a = dbObjectActionFor("drop", sch, ""); a.Object != "schema" || a.Name != "sales" {
		t.Fatalf("drop schema %+v", a)
	}
	if a = dbObjectActionFor("create-schema", sch, "staging"); a.Kind != dbclient.ActCreateSchema || a.Name != "staging" {
		t.Fatalf("create schema %+v", a)
	}
}

func TestDBObjectActionCardListsDependents(t *testing.T) {
	text := dbObjectActionCardText([]string{`DROP TABLE "sales"."orders" CASCADE`}, []string{"view sales.big_orders", "foreign key sales.items.items_order_fk"}, true)
	for _, w := range []string{"DROP TABLE", "Also removed or broken (2)", "view sales.big_orders", "cannot be undone"} {
		if !strings.Contains(text, w) {
			t.Errorf("no %q in\n%s", w, text)
		}
	}
	if strings.Contains(dbObjectActionCardText([]string{"CREATE SCHEMA x"}, nil, false), "Also removed") {
		t.Error("no dependents section without dependents")
	}
}
