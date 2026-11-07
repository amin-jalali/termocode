package dbclient

import (
	"strings"
	"testing"
)

func TestObjectDDLQueryKinds(t *testing.T) {
	for _, tc := range []struct {
		driver string
		kind   ObjectKind
		want   string
	}{
		{"postgres", ObjectSequence, "CREATE SEQUENCE"},
		{"postgres", ObjectMatView, "CREATE MATERIALIZED VIEW"},
		{"postgres", ObjectView, "pg_get_viewdef"},
		{"postgres", ObjectFunction, "pg_get_functiondef"},
		{"postgres", ObjectTrigger, "pg_get_triggerdef"},
		{"mysql", ObjectFunction, "SHOW CREATE FUNCTION"},
		{"sqlite", ObjectTrigger, "sqlite_master"},
	} {
		q, err := ObjectDDLQuery(tc.driver, tc.kind, "public", "obj")
		if err != nil {
			t.Fatalf("%s %s: %v", tc.driver, tc.kind, err)
		}
		if !strings.Contains(q.SQL, tc.want) {
			t.Fatalf("%s %s: %q does not contain %q", tc.driver, tc.kind, q.SQL, tc.want)
		}
	}
	if _, err := ObjectDDLQuery("sqlite", ObjectSequence, "", "s"); err == nil {
		t.Fatal("sqlite has no sequences: want an error")
	}
	if _, err := ObjectDDLQuery("postgres", ObjectSequence, "public", " "); err == nil {
		t.Fatal("empty name: want an error")
	}
}

func TestObjectDDLQueryBindsNames(t *testing.T) {
	q, err := ObjectDDLQuery("postgres", ObjectSequence, "app", "ids'; DROP TABLE x")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q.SQL, "DROP TABLE") {
		t.Fatalf("name went into the SQL text: %q", q.SQL)
	}
	if len(q.Args) != 2 || q.Args[0] != "app" {
		t.Fatalf("args = %v, want the schema and the name bound", q.Args)
	}
}

func TestCatalogQueriesPartitionsAndRoles(t *testing.T) {
	for _, d := range []Dialect{PostgresDialect{}, MySQLDialect{}} {
		q := d.CatalogQueries()
		if q["partitions"] == "" || q["roles"] == "" {
			t.Fatalf("%T: partitions/roles catalog queries missing", d)
		}
	}
}
