package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBFKWhere(t *testing.T) {
	got, err := dbFKWhere("postgres", []string{"id", "org"}, []dbclient.Cell{{Text: "7", Kind: "number"}, {Text: "it's"}})
	if err != nil || got != `"id" = 7 AND "org" = 'it''s'` {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := dbWhereCheck("postgres", got); err != nil {
		t.Fatalf("the filter check refuses %s: %v", got, err)
	}
	if _, err := dbFKWhere("postgres", []string{"id"}, []dbclient.Cell{{Null: true}}); err == nil {
		t.Fatal("a NULL key was followed")
	}
	if _, err := dbFKWhere("postgres", nil, nil); err == nil {
		t.Fatal("an empty key was followed")
	}
}

func TestDBSiblingTable(t *testing.T) {
	from := dbTable{conn: "c", driver: "postgres", sessKey: "c/db/app", schema: "public", name: "users", path: "c/db/app/schema/public/Tables/users"}
	to := dbSiblingTable(from, "orgs")
	if to.name != "orgs" || to.schema != "public" || to.sessKey != "c/db/app" || to.path != "c/db/app/schema/public/Tables/orgs" {
		t.Fatalf("got %+v", to)
	}
	if got := to.request().Database; got != "app" {
		t.Fatalf("database %q", got)
	}
}

func TestDBCanonFKsAndRefChoices(t *testing.T) {
	cs := dbclient.CanonSchema{Tables: []dbclient.CanonTable{
		{Name: "users"},
		{Name: "orders", Constraints: []dbclient.CanonConstraint{
			{Kind: "PRIMARY KEY", Columns: []string{"id"}},
			{Kind: "FOREIGN KEY", Name: "orders_user_fk", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}},
		}},
		{Name: "items", Constraints: []dbclient.CanonConstraint{
			{Kind: "FOREIGN KEY", Columns: []string{"a", "b"}, RefTable: "users", RefColumns: []string{"id", "org"}},
		}},
	}}
	all := dbCanonFKs(cs)
	if len(all["items"]) != 2 || all["items"][0].Name == "" || all["items"][0].Name != all["items"][1].Name {
		t.Fatalf("an unnamed key lost its columns: %+v", all["items"])
	}
	cells := map[string]dbclient.Cell{"id": {Text: "5", Kind: "number"}, "org": {Text: "x"}}
	refs := dbFKRefChoices("postgres", "users", all, cells)
	if len(refs) != 2 {
		t.Fatalf("got %+v", refs)
	}
	if refs[0].table != "items" || refs[0].where != `"a" = 5 AND "b" = 'x'` {
		t.Fatalf("items: %+v", refs[0])
	}
	if refs[1].table != "orders" || refs[1].where != `"user_id" = 5` || refs[1].label != "orders (user_id → id)" {
		t.Fatalf("orders: %+v", refs[1])
	}
	delete(cells, "org") // a key whose columns are not all in the row is left out
	if refs := dbFKRefChoices("postgres", "users", all, cells); len(refs) != 1 || refs[0].table != "orders" {
		t.Fatalf("got %+v", refs)
	}
}

func TestDBFKBackTrailIsCapped(t *testing.T) {
	var st dbFKState
	first := &dbTableView{}
	st.pushBack(dbFKBack{tv: first})
	for i := 0; i < dbFKBackMax+5; i++ {
		st.pushBack(dbFKBack{tv: &dbTableView{}, row: i})
	}
	if len(st.back) != dbFKBackMax || st.back[0].tv == first || st.back[len(st.back)-1].row != dbFKBackMax+4 {
		t.Fatalf("trail %d", len(st.back))
	}
}

func TestDBPageKeyRowsListFKKeys(t *testing.T) {
	want := map[string]bool{"gf": false, "gb": false, "gr": false, "gl": false}
	for _, r := range dbPageKeyRows() {
		if _, ok := want[r.Key]; ok {
			want[r.Key] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("the key card misses %s", k)
		}
	}
}
