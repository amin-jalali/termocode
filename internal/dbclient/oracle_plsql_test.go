package dbclient

import (
	"strings"
	"testing"
)

const plsqlBody = "CREATE OR REPLACE PACKAGE BODY pkg AS\n  PROCEDURE p IS\n  BEGIN\n    NULL;\n  END;\n  FUNCTION f RETURN NUMBER IS BEGIN RETURN 1; END;\nEND pkg;"

// DBX-63: a PL/SQL unit is one statement: its inner semicolons do not
// split it, it keeps its final ';', and a "/" line ends it (SQL*Plus).
func TestSplitOracleKeepsPLSQLWhole(t *testing.T) {
	src := "CREATE TABLE t (a NUMBER);\n" + plsqlBody + "\n/\nBEGIN\n  pkg.p;\nEND;\n/\nSELECT 1 FROM dual;"
	got, err := Split(src, "oracle")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CREATE TABLE t (a NUMBER)", plsqlBody, "BEGIN\n  pkg.p;\nEND;", "SELECT 1 FROM dual"}
	if strings.Join(got, "\n#\n") != strings.Join(want, "\n#\n") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if _, sts := Classify(plsqlBody, "oracle"); len(sts) != 1 {
		t.Fatalf("Classify split the package body into %d statements", len(sts))
	}
}

func TestObjectSourceQueryOracle(t *testing.T) {
	cases := []struct {
		kind  ObjectKind
		want  string
		lines bool
	}{
		{ObjectPackage, "type IN ('PACKAGE')", true},
		{ObjectPackageBody, "type IN ('PACKAGE BODY')", true},
		{ObjectFunction, "type IN ('FUNCTION', 'PROCEDURE')", true},
		{ObjectProcedure, "type IN ('PROCEDURE', 'FUNCTION')", true},
		{ObjectTrigger, "type IN ('TRIGGER')", true},
		{ObjectView, "DBMS_METADATA.GET_DDL('VIEW'", false},
	}
	for _, c := range cases {
		q, err := ObjectSourceQuery("oracle", c.kind, "HR.PKG")
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if !strings.Contains(q.SQL, c.want) || q.Lines != c.lines || len(q.Args) != 2 || q.Args[0] != "PKG" || q.Args[1] != "HR" {
			t.Fatalf("%s: got %+v", c.kind, q)
		}
	}
	q, _ := ObjectSourceQuery("oracle", ObjectPackage, "PKG")
	if len(q.Args) != 1 || !strings.Contains(q.SQL, "CURRENT_SCHEMA") {
		t.Fatalf("no schema: %+v", q)
	}
}

// The source of a PL/SQL unit is its all_source lines after CREATE OR
// REPLACE.
func TestSourceQueryTextJoinsLines(t *testing.T) {
	q, _ := ObjectSourceQuery("oracle", ObjectPackage, "PKG")
	r := Result{Columns: []Column{{Name: "TEXT"}}, Rows: [][]Cell{{{Text: "PACKAGE pkg AS\n"}}, {{Text: "  PROCEDURE p;\n"}}, {{Text: "END pkg;"}}}}
	got, ok := q.Text(r)
	if !ok || got != "CREATE OR REPLACE PACKAGE pkg AS\n  PROCEDURE p;\nEND pkg;" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := q.Text(Result{}); ok {
		t.Fatal("no rows must be no source")
	}
}

func TestSubmitObjectSourceOraclePackageBody(t *testing.T) {
	src := "\n" + strings.Replace(plsqlBody, "PACKAGE BODY pkg", `EDITIONABLE PACKAGE BODY "HR"."PKG"`, 1) + "\n/\n"
	plan, err := SubmitObjectSource("oracle", ObjectPackageBody, "HR.PKG", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Statements) != 1 || !strings.HasSuffix(plan.Statements[0], "END pkg;") || strings.Contains(plan.Statements[0], "\n/") {
		t.Fatalf("plan %q", plan.Statements)
	}
	if _, err := SubmitObjectSource("oracle", ObjectPackage, "HR.PKG", src); err == nil {
		t.Fatal("a body submitted as the spec must be refused")
	}
	if _, err := SubmitObjectSource("oracle", ObjectPackageBody, "HR.OTHER", src); err == nil {
		t.Fatal("another package's body must be refused")
	}
	if _, err := SubmitObjectSource("oracle", ObjectPackageBody, "HR.PKG", src+"DROP TABLE t;"); err == nil {
		t.Fatal("a second statement must be refused")
	}
}

func TestSourceHeaderKind(t *testing.T) {
	for src, want := range map[string]ObjectKind{
		"CREATE OR REPLACE PROCEDURE p IS BEGIN NULL; END;":      ObjectProcedure,
		"create or replace editionable function f return number": ObjectFunction,
		"CREATE PACKAGE BODY x AS END;":                          ObjectPackageBody,
		"CREATE OR REPLACE PACKAGE x AS END;":                    ObjectPackage,
	} {
		if got, ok := SourceHeaderKind(src); !ok || got != want {
			t.Errorf("%q = %q %v", src, got, ok)
		}
	}
}

// Compile errors come from all_errors; their lines map onto the buffer
// (leading blank lines move them down).
func TestOracleCompileErrors(t *testing.T) {
	q, err := OracleCompileErrorsQuery(ObjectPackageBody, "HR.PKG")
	if err != nil || !strings.Contains(q.SQL, "all_errors") || len(q.Args) != 3 || q.Args[2] != "PACKAGE BODY" {
		t.Fatalf("query %+v %v", q, err)
	}
	r := Result{Rows: [][]Cell{
		{{Text: "4"}, {Text: "5"}, {Text: "PLS-00103: Encountered the symbol \"END\""}, {Text: "ERROR"}},
		{{Text: "1"}, {Text: "1"}, {Text: "PLW-05018: unit has no AUTHID"}, {Text: "WARNING"}},
	}}
	got := OracleCompileErrors(r, "\n\n"+plsqlBody)
	if len(got) != 2 || got[0].Line != 6 || got[0].Col != 5 || got[0].Warning || !got[1].Warning || got[1].Line != 3 || !strings.Contains(got[0].Text, "PLS-00103") {
		t.Fatalf("got %+v", got)
	}
	if got[1].Col != 1+len("CREATE OR REPLACE ") {
		t.Fatalf("line 1 column not moved past CREATE OR REPLACE: %+v", got[1])
	}
}
