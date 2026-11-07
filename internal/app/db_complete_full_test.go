package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-3: the schema loaded in the background joins the payload the
// omnifunc reads: all objects, columns with types, foreign keys.
func TestDBSchemaPayloadMergesBackgroundSchema(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{}}
	before := s.schemaVer
	s.setFullSchema("local", dbclient.CompletionSchema{
		Tables: []dbclient.CompletionTable{{Schema: "public", Name: "users", Kind: "table"}, {Schema: "public", Name: "orders", Kind: "table"}, {Schema: "public", Name: "refresh", Kind: "func"}},
		Cols: map[string][]dbclient.Column{
			"users":  {{Name: "id", Type: "integer"}, {Name: "email", Type: "text"}},
			"orders": {{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "integer"}},
		},
		FKs: []dbclient.CompletionFK{{Table: "orders", Column: "user_id", RefTable: "users", RefColumn: "id"}},
	})
	if s.schemaVer == before {
		t.Fatal("schema version not bumped")
	}
	// A column list the user loaded by hand wins over the background one.
	s.setColumns("local", "orders", []string{"id", "user_id", "total"})
	p := s.schemaPayload(dbclient.Conn{ID: "local", Driver: "postgres"})
	tables := p["tables"].([]map[string]any)
	names := map[string]string{}
	for _, tb := range tables {
		names[tb["n"].(string)] = tb["k"].(string)
	}
	if names["users"] != "table" || names["refresh"] != "func" {
		t.Fatalf("tables: %v", tables)
	}
	cols := p["cols"].(map[string][]string)
	if len(cols["users"]) != 2 || len(cols["orders"]) != 3 {
		t.Fatalf("cols: %v", cols)
	}
	types := p["types"].(map[string]map[string]string)
	if types["users"]["email"] != "text" || types["users"]["id"] != "num" {
		t.Fatalf("types: %v", types)
	}
	fks := p["fks"].([]map[string]any)
	if len(fks) != 1 || fks[0]["t"] != "orders" || fks[0]["rt"] != "users" || fks[0]["c"] != "user_id" || fks[0]["rc"] != "id" {
		t.Fatalf("fks: %v", fks)
	}
	if p2 := s.schemaPayload(dbclient.Conn{ID: "other", Driver: "postgres"}); len(p2["fks"].([]map[string]any)) != 0 {
		t.Fatalf("other connection got fks: %v", p2["fks"])
	}
	s.resetConnectionTree("local")
	if s.full["local"] != nil {
		t.Fatal("reset kept the background schema")
	}
}
