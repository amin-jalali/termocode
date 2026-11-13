package app

import (
	"strings"
	"testing"
)

// DBX-67: the AI's db tools reach a Cassandra connection (its own
// session, not the SQL driver), read a keyspace and refuse a write.
func TestAIDBToolsCassandra(t *testing.T) {
	e := newAIDBEnv(t)
	writeCQLConns(t, e.root)
	e.m.databases.load(e.root)
	stub := shopStub()
	useCQLStub(t, stub)

	out, err := e.run(t, "db_query", map[string]any{"conn": "wide", "sql": "SELECT id, name FROM shop.orders", "max_rows": 2})
	if err != nil || !strings.Contains(out, "id") || !strings.Contains(out, "a") {
		t.Fatalf("query: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "wide"})
	if err != nil || !strings.Contains(out, "shop") || !strings.Contains(out, "system") {
		t.Fatalf("keyspaces: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "wide", "database": "shop"})
	if err != nil || !strings.Contains(out, "orders (table)") || !strings.Contains(out, "users (table)") {
		t.Fatalf("tables of a keyspace: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "wide", "database": "shop", "table": "orders"})
	if err != nil || !strings.Contains(out, "id int [PK]") {
		t.Fatalf("columns: %q %v", out, err)
	}
	if mut, _, err := e.gate(t, "db_query", map[string]any{"conn": "wide", "sql": "DROP TABLE shop.orders"}); err != nil || !mut {
		t.Fatalf("a DROP must ask: %v %v", mut, err)
	}
}
