package app

import "testing"

func TestDBERDiagramPaletteItem(t *testing.T) {
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-er-diagram" {
			if it.Title != "Databases: Export ER Diagram…" {
				t.Fatalf("title %q", it.Title)
			}
			return
		}
	}
	t.Fatal("no db-er-diagram palette item")
}

// The diagram is of the schema the tree cursor is on or inside.
func TestDBERSchemaOfRow(t *testing.T) {
	nodes := map[string]dbNode{
		"pg":                                    {kind: dbNodeConnection},
		"pg/db/shop":                            {kind: dbNodeDatabase, name: "shop"},
		"pg/db/shop/schema/sales":               {kind: dbNodeSchema, name: "sales"},
		"pg/db/shop/schema/sales/Tables":        {kind: dbNodeGroup, name: "Tables"},
		"pg/db/shop/schema/sales/Tables/orders": {kind: dbNodeTable, name: "orders"},
		"my":                                    {kind: dbNodeConnection},
		"my/db/app":                             {kind: dbNodeDatabase, name: "app"},
		"my/db/app/Tables/t":                    {kind: dbNodeTable, name: "t"},
	}
	cases := []struct{ path, driver, want string }{
		{"pg/db/shop/schema/sales/Tables/orders", "postgres", "sales"},
		{"pg/db/shop/schema/sales", "postgres", "sales"},
		{"pg/db/shop", "postgres", ""},
		{"my/db/app/Tables/t", "mysql", "app"},
		{"my/db/app", "mysql", "app"},
		{"lite", "sqlite", "main"},
	}
	for _, c := range cases {
		if got := dbERSchemaOf(nodes, c.path, c.driver); got != c.want {
			t.Errorf("%s (%s) = %q, want %q", c.path, c.driver, got, c.want)
		}
	}
}
