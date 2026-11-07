package dbclient

import (
	"context"
	"errors"
	"testing"
)

type fakeIntrospector struct {
	calls []IntrospectionRequest
	res   func(IntrospectionRequest) (IntrospectionResult, error)
}

func (f *fakeIntrospector) Introspect(_ context.Context, r IntrospectionRequest) (IntrospectionResult, error) {
	f.calls = append(f.calls, r)
	return f.res(r)
}

// DBX-3: the whole schema loads for completion, with column types and the
// foreign keys; system schemas are skipped; a broken object is skipped.
func TestLoadCompletionSchemaPostgres(t *testing.T) {
	f := &fakeIntrospector{res: func(r IntrospectionRequest) (IntrospectionResult, error) {
		switch {
		case r.Database == "current":
			return IntrospectionResult{Schemas: []string{"public", "pg_catalog", "information_schema", "audit"}}, nil
		case r.Object == "" && r.Schema == "public":
			return IntrospectionResult{Objects: []SchemaObject{{Name: "users", Kind: "table"}, {Name: "orders", Kind: "BASE TABLE"}, {Name: "active", Kind: "view"}}, Routines: []string{"refresh"}}, nil
		case r.Object == "" && r.Schema == "audit":
			return IntrospectionResult{Objects: []SchemaObject{{Name: "log", Kind: "table"}}}, nil
		case r.Object == "users":
			return IntrospectionResult{Columns: []Column{{Name: "id", Type: "integer", PK: true}, {Name: "email", Type: "text"}}}, nil
		case r.Object == "orders":
			return IntrospectionResult{Columns: []Column{{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "integer"}},
				ForeignKeys: []SchemaForeignKey{{Name: "fk", Column: "user_id", RefTable: "public.Users", RefColumn: "id"}}}, nil
		case r.Object == "log":
			return IntrospectionResult{}, errors.New("denied")
		}
		return IntrospectionResult{}, nil
	}}
	got, err := LoadCompletionSchema(context.Background(), f, "postgres", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c.Schema == "pg_catalog" || c.Schema == "information_schema" {
			t.Fatalf("system schema read: %+v", c)
		}
	}
	if len(got.Tables) != 5 {
		t.Fatalf("tables: %+v", got.Tables)
	}
	if c := got.Cols["users"]; len(c) != 2 || c[1].Type != "text" {
		t.Fatalf("users columns: %+v", c)
	}
	if len(got.FKs) != 1 || got.FKs[0] != (CompletionFK{Table: "orders", Column: "user_id", RefTable: "users", RefColumn: "id"}) {
		t.Fatalf("fks: %+v", got.FKs)
	}
	if _, ok := got.Cols["log"]; ok || got.Partial {
		t.Fatalf("broken table kept or partial: %+v", got)
	}
}

func TestLoadCompletionSchemaLimitAndMySQL(t *testing.T) {
	f := &fakeIntrospector{res: func(r IntrospectionRequest) (IntrospectionResult, error) {
		if r.Object == "" {
			return IntrospectionResult{Objects: []SchemaObject{{Name: "a", Kind: "table"}, {Name: "b", Kind: "table"}}}, nil
		}
		return IntrospectionResult{Columns: []Column{{Name: "x", Type: "int"}}}, nil
	}}
	got, err := LoadCompletionSchema(context.Background(), f, "mysql", "shop", 1)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Schema != "shop" || len(got.Cols) != 1 || !got.Partial {
		t.Fatalf("calls %+v got %+v", f.calls, got)
	}
	empty, err := LoadCompletionSchema(context.Background(), f, "mysql", "", 1)
	if err != nil || len(empty.Tables) != 0 {
		t.Fatalf("no database: %+v %v", empty, err)
	}
}

func TestTypeFamily(t *testing.T) {
	for in, want := range map[string]string{
		"integer": "num", "int(11)": "num", "double precision": "num", "numeric(10,2)": "num", "bigserial": "num",
		"character varying(20)": "text", "varchar(255)": "text", "TEXT": "text",
		"timestamp with time zone": "time", "date": "time", "boolean": "bool", "jsonb": "json",
		"uuid": "uuid", "bytea": "bin", "": "", "geometry": "",
	} {
		if got := TypeFamily(in); got != want {
			t.Errorf("TypeFamily(%q) = %q, want %q", in, got, want)
		}
	}
}
