package dbclient

import (
	"context"
	"database/sql/driver"
	"testing"
)

// Every catalog query answers one row of four cells: enough for each
// kind of catalog read (columns, keys, indexes, foreign keys, partitions,
// checks…) to take it in.
func catalogFake() *fakeSQL {
	return (&fakeSQL{}).
		on("select", fakeSet{cols: []string{"a", "b", "c", "d"}, rows: [][]driver.Value{{"x", "int", "true", "y"}, {"z"}}}).
		on("with", fakeSet{cols: []string{"a", "b", "c", "d"}, rows: [][]driver.Value{{"x", "int", "true", "y"}}})
}

func TestSQLSessionIntrospectEveryLevelPerEngine(t *testing.T) {
	ctx := context.Background()
	for _, e := range []struct {
		name string
		d    Dialect
	}{
		{"postgres", PostgresDialect{}}, {"mysql", MySQLDialect{}}, {"sqlserver", SQLServerDialect{}}, {"oracle", OracleDialect{}},
		{"firebird", FirebirdDialect{}}, {"exasol", ExasolDialect{}}, {"vertica", VerticaDialect{}}, {"hana", HANADialect{}},
	} {
		s := fakeSession(t, catalogFake(), e.d, e.name, false)
		for _, req := range []IntrospectionRequest{
			{},
			{Database: "d"},
			{Schema: "s"},
			{Schema: "s", Object: "t"},
			{Schema: "s", Object: "t", Lean: true},
			{Object: "t"},
			{Schema: "s", Object: "t", Kind: "view"},
		} {
			if _, err := s.Introspect(ctx, req); err != nil {
				t.Errorf("%s %+v: %v", e.name, req, err)
			}
		}
	}
	// A catalog query that fails is the error of the read.
	bad := (&fakeSQL{}).fail("select", errFake).fail("with", errFake)
	s := fakeSession(t, bad, PostgresDialect{}, "postgres", false)
	for _, req := range []IntrospectionRequest{{}, {Schema: "s"}, {Schema: "s", Object: "t"}, {Object: "t"}} {
		if _, err := s.Introspect(ctx, req); err == nil {
			t.Errorf("%+v: a failing catalog must be an error", req)
		}
	}
	// A preset's own queries are used.
	pre := newSQLSession(catalogFake().open(t), PostgresDialect{}, "postgres", Conn{Driver: "postgres", Preset: "cockroach"}, false)
	if _, err := pre.Introspect(ctx, IntrospectionRequest{Schema: "s", Object: "t"}); err != nil {
		t.Errorf("preset = %v", err)
	}
}
