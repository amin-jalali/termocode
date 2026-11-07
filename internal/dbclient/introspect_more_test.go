package dbclient

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// DBX-13: the schema level lists triggers (and PostgreSQL extensions),
// the object level lists check constraints, and PostgreSQL's object
// list holds materialized views. Every new query binds the names.
func TestIntrospectionReadsTriggersExtensionsAndChecks(t *testing.T) {
	for label, c := range map[string]struct {
		dialect Dialect
		name    string
		ext     bool
	}{"postgres": {PostgresDialect{}, "postgres", true}, "mysql": {MySQLDialect{}, "mysql", false}} {
		stub := &dbtest.Stub{Columns: []string{"a", "b"}, ColumnTypes: []string{"VARCHAR", "TEXT"}, Rows: [][]driver.Value{{[]byte("x"), []byte("y")}}}
		db := dbtest.Open(fmt.Sprintf("dbx13-%s", label), stub)
		s := newSQLSession(db, c.dialect, c.name, Conn{}, false)
		res, err := s.Introspect(context.Background(), IntrospectionRequest{Schema: "public"})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if !reflect.DeepEqual(res.Triggers, []string{"x"}) {
			t.Errorf("%s triggers = %v", label, res.Triggers)
		}
		if c.ext != (len(res.Extensions) == 1) {
			t.Errorf("%s extensions = %v", label, res.Extensions)
		}
		obj, err := s.Introspect(context.Background(), IntrospectionRequest{Schema: "public", Object: "orders"})
		if err != nil {
			t.Fatalf("%s object: %v", label, err)
		}
		if !reflect.DeepEqual(obj.Checks, []SchemaCheck{{Name: "x", Expr: "y"}}) {
			t.Errorf("%s checks = %+v", label, obj.Checks)
		}
		for i, st := range stub.Recorded() {
			if strings.HasPrefix(st, "SHOW CREATE TABLE") {
				continue // MySQL cannot bind SHOW; the name is escaped by QuoteIdent
			}
			if strings.Contains(st, "orders") || strings.Contains(st, "'public'") {
				t.Errorf("%s: name spliced into SQL: %s", label, st)
			}
			if strings.Contains(strings.ToLower(st), "trigger") && !reflect.DeepEqual(stub.RecordedArgs()[i], []any{"public"}) {
				t.Errorf("%s: trigger query args %v", label, stub.RecordedArgs()[i])
			}
		}
		_ = s.Close()
		_ = db.Close()
	}
	for _, key := range []string{"triggers", "checks"} {
		if (MySQLDialect{}).CatalogQueries()[key] == "" {
			t.Errorf("mysql has no %s query", key)
		}
	}
	if q := (PostgresDialect{}).CatalogQueries()["objects"]; !strings.Contains(q, "pg_matviews") {
		t.Errorf("postgres objects miss materialized views: %s", q)
	}
}
