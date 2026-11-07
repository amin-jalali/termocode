package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

var fkNavKeys = []SchemaForeignKey{
	{Name: "orders_user_fk", Column: "user_id", RefTable: "users", RefColumn: "id"},
	{Name: "orders_item_fk", Column: "shop", RefTable: "items", RefColumn: "shop_id"},
	{Name: "orders_item_fk", Column: "sku", RefTable: "items", RefColumn: "sku"},
}

func TestFKForColumnFindsTheWholeKey(t *testing.T) {
	g, ok := FKForColumn(fkNavKeys, "sku")
	if !ok || len(g) != 2 || g[0].Column != "shop" || g[1].Column != "sku" {
		t.Fatalf("group = %+v ok=%v", g, ok)
	}
	if g, ok := FKForColumn(fkNavKeys, "USER_ID"); !ok || len(g) != 1 {
		t.Fatalf("column match must ignore case: %+v", g)
	}
	if _, ok := FKForColumn(fkNavKeys, "total"); ok {
		t.Fatalf("a plain column has no key")
	}
}

func TestFKTargetQueryBindsEveryValue(t *testing.T) {
	g, _ := FKForColumn(fkNavKeys, "sku")
	row := map[string]Cell{"shop": {Text: "7"}, "sku": {Text: "a'b"}, "total": {Text: "9"}}
	st, err := FKTargetQuery(PostgresDialect{}, "public", g, row)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM "public"."items" WHERE "shop_id" = $1 AND "sku" = $2 LIMIT 1 OFFSET 0`
	if st.SQL != want {
		t.Fatalf("sql = %q\nwant  %q", st.SQL, want)
	}
	if !reflect.DeepEqual(st.Args, []any{"7", "a'b"}) {
		t.Fatalf("args = %#v", st.Args)
	}
	if strings.Contains(st.SQL, "a'b") {
		t.Fatalf("values must be bound, not written into the SQL")
	}
}

func TestFKTargetQueryNullOrMissingValue(t *testing.T) {
	g, _ := FKForColumn(fkNavKeys, "user_id")
	if _, err := FKTargetQuery(SQLiteDialect{}, "", g, map[string]Cell{"user_id": {Null: true}}); err == nil {
		t.Fatalf("a NULL foreign key points nowhere: want an error")
	}
	if _, err := FKTargetQuery(SQLiteDialect{}, "", g, map[string]Cell{}); err == nil {
		t.Fatalf("a missing column: want an error")
	}
	if _, err := FKTargetQuery(nil, "", g, map[string]Cell{"user_id": {Text: "1"}}); err == nil {
		t.Fatalf("no dialect: want an error")
	}
}

func TestReferencingRowsQuery(t *testing.T) {
	g, _ := FKForColumn(fkNavKeys, "shop")
	parent := map[string]Cell{"shop_id": {Text: "7"}, "sku": {Text: "x"}}
	st, err := ReferencingRowsQuery(MySQLDialect{}, "", "orders", g, parent, 200)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM `orders` WHERE `shop` = ? AND `sku` = ? LIMIT 200 OFFSET 0"
	if !strings.HasPrefix(st.SQL, "SELECT * FROM `orders` WHERE `shop` = ? AND `sku` = ?") {
		t.Fatalf("sql = %q\nwant  %q", st.SQL, want)
	}
	if !reflect.DeepEqual(st.Args, []any{"7", "x"}) {
		t.Fatalf("args = %#v", st.Args)
	}
}

func TestIncomingFKsGroupsKeysThatPointAtATable(t *testing.T) {
	all := map[string][]SchemaForeignKey{
		"orders":   fkNavKeys,
		"payments": {{Name: "pay_user_fk", Column: "payer", RefTable: "users", RefColumn: "id"}},
		"users":    nil,
	}
	got := IncomingFKs(all, "users")
	if len(got) != 2 || got[0].Table != "orders" || got[1].Table != "payments" || got[1].Key[0].Column != "payer" {
		t.Fatalf("incoming = %+v", got)
	}
}

func TestFKLookupQuery(t *testing.T) {
	g, _ := FKForColumn(fkNavKeys, "user_id")
	st, err := FKLookupQuery(PostgresDialect{}, "public", g, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT DISTINCT "id" FROM "public"."users" ORDER BY "id" LIMIT 50 OFFSET 0`
	if st.SQL != want {
		t.Fatalf("sql = %q\nwant  %q", st.SQL, want)
	}
	g2, _ := FKForColumn(fkNavKeys, "sku")
	if _, err := FKLookupQuery(PostgresDialect{}, "", g2, 50); err == nil {
		t.Fatalf("lookup of a two-column key is not supported: want an error")
	}
}

func TestFKTrailBack(t *testing.T) {
	var tr FKTrail
	if _, ok := tr.Back(); ok {
		t.Fatalf("empty trail has nothing to go back to")
	}
	tr.Push(FKStep{Table: "orders", Row: 3, Column: "user_id"})
	tr.Push(FKStep{Table: "users", Row: 0, Column: "org_id"})
	if tr.Len() != 2 {
		t.Fatalf("len = %d", tr.Len())
	}
	s, ok := tr.Back()
	if !ok || s.Table != "users" {
		t.Fatalf("back = %+v", s)
	}
	s, _ = tr.Back()
	if s.Table != "orders" || s.Row != 3 || s.Column != "user_id" {
		t.Fatalf("back = %+v", s)
	}
	for i := 0; i < fkTrailMax+10; i++ {
		tr.Push(FKStep{Table: "t", Row: i})
	}
	if tr.Len() != fkTrailMax {
		t.Fatalf("trail must be capped at %d, got %d", fkTrailMax, tr.Len())
	}
}
