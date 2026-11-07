package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBVirtualPathIsPerConnectionInProject(t *testing.T) {
	if got := dbVirtualPath("/w", "shop"); got != filepath.Join("/w", ".termocode", "virtual", "shop.jsonc") {
		t.Fatalf("path %q", got)
	}
	if dbVirtualPath("/w", "../x") != "" {
		t.Fatal("a bad connection id must give no path")
	}
}

func TestDBVirtualPaletteItem(t *testing.T) {
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-virtual-objects" {
			if it.Title != "Databases: Edit Virtual Objects…" {
				t.Fatalf("title %q", it.Title)
			}
			return
		}
	}
	t.Fatal("no db-virtual-objects palette item")
}

func TestDBApplyVirtualAddsColumnsAndKeys(t *testing.T) {
	v, _, err := dbclient.ParseVirtualObjects([]byte(`{
		"foreignKeys": [{"table":"orders","column":"buyer","refTable":"users","refColumn":"id"}],
		"nameRules": [{"suffix":"_id"}],
		"columns": [{"table":"orders","name":"total_eur","expr":"total * 0.9"}]
	}`), "postgres")
	if err != nil {
		t.Fatal(err)
	}
	in := dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id"}, {Name: "buyer"}, {Name: "shop_id"}, {Name: "total"}}}
	out := dbApplyVirtual(v, dbNodeTable, "orders", []string{"users", "shops", "orders"}, in)
	var fks []string
	for _, fk := range out.ForeignKeys {
		fks = append(fks, fk.Column+"→"+fk.RefTable)
	}
	if strings.Join(fks, ",") != "buyer→users,shop_id→shops" {
		t.Fatalf("virtual keys %v", fks)
	}
	last := out.Columns[len(out.Columns)-1]
	if last.Name != "total_eur" || !strings.Contains(last.Type, "virtual") {
		t.Fatalf("virtual column %+v", last)
	}
}

func TestDBVirtualViewsUnderConnection(t *testing.T) {
	s := newDBState()
	s.nodes["c"] = dbNode{kind: dbNodeConnection, name: "c", path: "c", conn: "c"}
	v, _, _ := dbclient.ParseVirtualObjects([]byte(`{"views":[{"name":"big_orders","sql":"SELECT * FROM orders WHERE total > 100"}]}`), "postgres")
	s.addVirtualViews("c", "c", v)
	n, ok := s.nodes["c/Virtual Views/big_orders"]
	if !ok || n.kind != dbNodeVirtualView {
		t.Fatalf("no virtual view node: %+v", s.nodes)
	}
	if !strings.Contains(strings.Join(s.nodes["c"].children, ","), "c/Virtual Views") {
		t.Fatalf("group not under the connection: %v", s.nodes["c"].children)
	}
}

func TestDBVirtualTemplateParses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.jsonc")
	if err := os.WriteFile(p, []byte(dbVirtualTemplate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, problems, err := dbclient.ParseVirtualObjects([]byte(dbVirtualTemplate), "postgres"); err != nil || len(problems) > 0 {
		t.Fatalf("template: %v %v", err, problems)
	}
}

// DBX-31: the data view's FK navigation knows the virtual keys: gf follows
// one virtual column alone, and gr lists the tables whose virtual keys
// point at the row (marked virtual).
func TestDBVirtualKeysInFKNavigation(t *testing.T) {
	v, _, err := dbclient.ParseVirtualObjects([]byte(`{
		"foreignKeys": [{"table":"orders","column":"buyer","refTable":"users","refColumn":"id"}],
		"nameRules": [{"suffix":"_id"}]
	}`), "postgres")
	if err != nil {
		t.Fatal(err)
	}
	in := dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id"}, {Name: "buyer"}, {Name: "shop_id"}}}
	fks := dbApplyVirtual(v, dbNodeTable, "orders", []string{"users", "shops", "orders"}, in).ForeignKeys
	key, ok := dbclient.FKForColumn(fks, "buyer")
	if !ok || len(key) != 1 || key[0].RefTable != "users" {
		t.Fatalf("gf on buyer: %+v %v", key, ok)
	}
	cs := dbclient.CanonSchema{Tables: []dbclient.CanonTable{
		{Name: "users", Columns: []dbclient.CanonColumn{{Name: "id"}}},
		{Name: "orders", Columns: []dbclient.CanonColumn{{Name: "id"}, {Name: "buyer"}}},
	}}
	cells := map[string]dbclient.Cell{"id": {Text: "5", Kind: "number"}}
	refs := dbFKRefChoices("postgres", "users", dbCanonFKs(v.WithForeignKeys(cs)), cells)
	if len(refs) != 1 || refs[0].table != "orders" || refs[0].where != `"buyer" = 5` || refs[0].label != "orders (buyer → id) · virtual" {
		t.Fatalf("gr refs %+v", refs)
	}
}
