//go:build dbint

package dbclient

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

// A live PostgreSQL (TERMOCODE_DBINT_PG_URL): composite keys and column
// names with spaces must come back whole and once.
func TestPostgresIntrospectCompositeKeysAndSpacedColumns(t *testing.T) {
	url := os.Getenv("TERMOCODE_DBINT_PG_URL")
	if url == "" {
		t.Skip("TERMOCODE_DBINT_PG_URL is not set")
	}
	conn, err := ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := NewSQLSession(ctx, conn, conn.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS introspect_t CASCADE`,
		`CREATE SCHEMA introspect_t`,
		`CREATE TABLE introspect_t.orgs (id bigint, part int, PRIMARY KEY (id, part))`,
		`CREATE TABLE introspect_t.items (id bigint PRIMARY KEY, org_id bigint, org_part int, "Item Name" text, "b,c" text, FOREIGN KEY (org_id, org_part) REFERENCES introspect_t.orgs (id, part))`,
		`CREATE INDEX items_idx ON introspect_t.items ("Item Name", "b,c", id)`,
		`CREATE INDEX items_one_idx ON introspect_t.items ("Item Name")`,
	} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
	}
	defer s.Run(context.Background(), `DROP SCHEMA IF EXISTS introspect_t CASCADE`, RunOptions{})
	got, err := s.Introspect(ctx, IntrospectionRequest{Schema: "introspect_t", Object: "items"})
	if err != nil {
		t.Fatal(err)
	}
	for name, cols := range map[string][]string{"items_idx": {"Item Name", "b,c", "id"}, "items_one_idx": {"Item Name"}, "items_pkey": {"id"}} {
		var idx *SchemaIndex
		for i := range got.Indexes {
			if got.Indexes[i].Name == name {
				idx = &got.Indexes[i]
			}
		}
		if idx == nil || !reflect.DeepEqual(idx.Columns, cols) {
			t.Errorf("%s columns = %+v, want %v", name, got.Indexes, cols)
		}
	}
	want := []SchemaForeignKey{
		{Name: "items_org_id_org_part_fkey", Column: "org_id", RefTable: "orgs", RefColumn: "id"},
		{Name: "items_org_id_org_part_fkey", Column: "org_part", RefTable: "orgs", RefColumn: "part"},
	}
	if !reflect.DeepEqual(got.ForeignKeys, want) {
		t.Errorf("foreign keys = %+v, want %+v", got.ForeignKeys, want)
	}
}
