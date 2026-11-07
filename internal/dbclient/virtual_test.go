package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

const virtualSample = `{
  // virtual objects live next to the connection, never in the database
  "foreignKeys": [{"table": "orders", "column": "buyer", "refTable": "users", "refColumn": "id"}],
  "nameRules": [{"suffix": "_id"}],
  "columns": [{"table": "orders", "name": "total_cents", "expr": "total * 100"}],
  "views": [{"name": "big_orders", "sql": "SELECT * FROM orders WHERE total > 100"}]
}`

func TestParseVirtualObjectsReadsAllKinds(t *testing.T) {
	v, problems, err := ParseVirtualObjects([]byte(virtualSample), "postgres")
	if err != nil || len(problems) != 0 {
		t.Fatalf("err=%v problems=%v", err, problems)
	}
	if len(v.ForeignKeys) != 1 || len(v.NameRules) != 1 || len(v.Columns) != 1 || len(v.Views) != 1 {
		t.Fatalf("parsed = %+v", v)
	}
	if v.NameRules[0].RefColumn != "id" {
		t.Errorf("name rule refColumn default = %q, want id", v.NameRules[0].RefColumn)
	}
}

func TestParseVirtualObjectsDropsUnsafeOrBrokenEntries(t *testing.T) {
	src := `{
  "foreignKeys": [{"table": "orders", "column": "", "refTable": "users", "refColumn": "id"}],
  "nameRules": [{"suffix": ""}],
  "columns": [{"table": "orders", "name": "x", "expr": "1); DROP TABLE orders; --"}],
  "views": [
    {"name": "wipe", "sql": "DELETE FROM orders"},
    {"name": "two", "sql": "SELECT 1; SELECT 2"},
    {"name": "ok", "sql": "SELECT 1"},
    {"name": "ok", "sql": "SELECT 2"}
  ]
}`
	v, problems, err := ParseVirtualObjects([]byte(src), "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.ForeignKeys) != 0 || len(v.NameRules) != 0 || len(v.Columns) != 0 {
		t.Errorf("broken entries kept: %+v", v)
	}
	if len(v.Views) != 1 || v.Views[0].Name != "ok" || v.Views[0].SQL != "SELECT 1" {
		t.Errorf("views = %+v, want only the first ok", v.Views)
	}
	if len(problems) != 6 {
		t.Errorf("problems = %d %v, want 6", len(problems), problems)
	}
	if _, _, err := ParseVirtualObjects([]byte("{"), "postgres"); err == nil {
		t.Error("bad JSON must fail")
	}
}

func TestVirtualForeignKeysForMergesExplicitAndNameRules(t *testing.T) {
	v, _, _ := ParseVirtualObjects([]byte(virtualSample), "postgres")
	cols := []Column{{Name: "id"}, {Name: "buyer"}, {Name: "user_id"}, {Name: "product_id"}, {Name: "shop_id"}, {Name: "ghost_id"}}
	real := []SchemaForeignKey{{Name: "fk_shop", Column: "shop_id", RefTable: "shops", RefColumn: "id"}}
	tables := []string{"orders", "users", "Products", "shops"}
	got := v.ForeignKeysFor("ORDERS", cols, real, tables)
	want := []SchemaForeignKey{
		{Name: "virtual", Column: "buyer", RefTable: "users", RefColumn: "id", Virtual: true},
		{Name: "virtual", Column: "user_id", RefTable: "users", RefColumn: "id", Virtual: true},
		{Name: "virtual", Column: "product_id", RefTable: "Products", RefColumn: "id", Virtual: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ForeignKeysFor =\n%+v\nwant\n%+v", got, want)
	}
	if got := v.ForeignKeysFor("users", []Column{{Name: "id"}}, nil, tables); len(got) != 0 {
		t.Errorf("users has no virtual FKs, got %+v", got)
	}
}

func TestVirtualColumnsAndViews(t *testing.T) {
	v, _, _ := ParseVirtualObjects([]byte(virtualSample), "postgres")
	if got := v.ColumnsFor("Orders"); len(got) != 1 || got[0].Name != "total_cents" {
		t.Errorf("ColumnsFor = %+v", got)
	}
	if got := v.ColumnsFor("users"); len(got) != 0 {
		t.Errorf("users virtual columns = %+v", got)
	}
	sel := v.SelectWithVirtualColumns("orders")
	if sel != `SELECT *, (total * 100) AS "total_cents" FROM "orders"` {
		t.Errorf("SelectWithVirtualColumns = %q", sel)
	}
	if k, _ := Classify(sel, "postgres"); k != KindRead {
		t.Errorf("virtual column select classifies as %s", k)
	}
	if got := v.ViewObjects(); !reflect.DeepEqual(got, []SchemaObject{{Name: "big_orders", Kind: VirtualViewKind}}) {
		t.Errorf("ViewObjects = %+v", got)
	}
	if sql, ok := v.ViewSQL("big_orders"); !ok || !strings.HasPrefix(sql, "SELECT") {
		t.Errorf("ViewSQL = %q %v", sql, ok)
	}
	if _, ok := v.ViewSQL("nope"); ok {
		t.Error("unknown view found")
	}
}
