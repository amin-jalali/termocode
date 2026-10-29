package dbclient

import (
	"strings"
	"testing"
)

func scriptHas(t *testing.T, script string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(script, w) {
			t.Errorf("script lacks %q:\n%s", w, script)
		}
	}
}

// Routines are listed for a person, never written: only in the source,
// only in the target, or different.
func TestCompareSchemasRoutinesAreReviewNotes(t *testing.T) {
	fn := CanonRoutine{Name: "bump", Kind: "FUNCTION", Args: "x integer", Returns: "integer", Language: "sql", Body: "SELECT x + 1"}
	changed := fn
	changed.Body = "SELECT x + 2"
	proc := CanonRoutine{Name: "clean", Kind: "PROCEDURE", Language: "plpgsql", Body: "BEGIN $$ END"}
	old := CanonRoutine{Name: "legacy", Kind: "FUNCTION", Language: "sql", Body: "SELECT 1"}
	src := CanonSchema{Driver: "postgres", Schema: "a", Routines: []CanonRoutine{fn, proc}}
	tgt := CanonSchema{Driver: "postgres", Schema: "b", Routines: []CanonRoutine{changed, old}}
	c := CompareSchemas(src, tgt)
	scriptHas(t, c.Script,
		"-- Routines (never written by this script)",
		`-- REVIEW: function "bump"(x integer) differs: see routines.sql.`,
		`-- REVIEW: procedure "clean"() is only in the source`,
		`-- REVIEW: function "legacy"() is only in the target`)
	if c.Changes != 3 || strings.Contains(c.Script, "CREATE FUNCTION") {
		t.Errorf("changes = %d, script:\n%s", c.Changes, c.Script)
	}
	var routines string
	for _, f := range c.Files {
		if f.Name == "routines.sql" {
			routines = f.New
		}
	}
	if !strings.Contains(routines, `CREATE FUNCTION "bump"(x integer) RETURNS integer LANGUAGE sql AS $$`) ||
		!strings.Contains(routines, "$body$\nBEGIN $$ END\n$body$;") {
		t.Errorf("routines.sql:\n%s", routines)
	}
	my := CanonFiles(CanonSchema{Driver: "mysql", Routines: []CanonRoutine{{Name: "f", Kind: "FUNCTION", Returns: "int", Body: "RETURN 1"}}})
	if got := my[2].Text; !strings.Contains(got, "CREATE FUNCTION `f`() RETURNS int\nRETURN 1;") {
		t.Errorf("mysql routines.sql:\n%s", got)
	}
}

// Index kinds keep their syntax per server; a materialized view is
// dropped and made as one.
func TestCompareSchemasIndexKindsAndMaterializedViews(t *testing.T) {
	tbl := CanonTable{Name: "t", Columns: []CanonColumn{{Name: "a", Type: "text"}}}
	pgIdx := []CanonIndex{
		{Table: "t", Name: "t_a_gin", Exprs: []string{"a gin_trgm_ops"}, Method: "gin"},
		{Table: "t", Name: "t_a_uniq", Exprs: []string{"a"}, Unique: true, Where: "a IS NOT NULL"},
	}
	src := CanonSchema{Driver: "postgres", Schema: "a", Tables: []CanonTable{withIndexes(tbl, pgIdx)}, Views: []CanonView{{Name: "mv", Definition: "SELECT 1", Materialized: true}}}
	tgt := CanonSchema{Driver: "postgres", Schema: "b", Tables: []CanonTable{tbl}, Views: []CanonView{{Name: "mv", Definition: "SELECT 2", Materialized: true}}}
	scriptHas(t, CompareSchemas(src, tgt).Script,
		`CREATE INDEX "t_a_gin" ON "b"."t" USING gin (a gin_trgm_ops);`,
		`CREATE UNIQUE INDEX "t_a_uniq" ON "b"."t" (a) WHERE a IS NOT NULL;`,
		`DROP MATERIALIZED VIEW "b"."mv";`,
		`CREATE MATERIALIZED VIEW "b"."mv" AS`)

	myIdx := []CanonIndex{
		{Table: "t", Name: "ft", Exprs: []string{"`a`"}, Method: "FULLTEXT"},
		{Table: "t", Name: "h", Exprs: []string{"`a`"}, Method: "HASH"},
	}
	src = CanonSchema{Driver: "mysql", Schema: "a", Tables: []CanonTable{withIndexes(tbl, myIdx)}}
	tgt = CanonSchema{Driver: "mysql", Schema: "b", Tables: []CanonTable{tbl}}
	scriptHas(t, CompareSchemas(src, tgt).Script,
		"CREATE FULLTEXT INDEX `ft` ON `b`.`t` (`a`);",
		"CREATE INDEX `h` ON `b`.`t` (`a`) USING HASH;")

	sqIdx := []CanonIndex{{Table: "t", Name: "t_a", Exprs: []string{`"a"`}}}
	src = CanonSchema{Driver: "sqlite", Schema: "main", Tables: []CanonTable{withIndexes(tbl, sqIdx)}}
	tgt = CanonSchema{Driver: "sqlite", Schema: "aux", Tables: []CanonTable{tbl}}
	scriptHas(t, CompareSchemas(src, tgt).Script, `CREATE INDEX "aux"."t_a" ON "t" ("a");`)
}

func withIndexes(t CanonTable, ix []CanonIndex) CanonTable {
	t.Indexes = ix
	return t
}

// Constraint changes on a table that stays are notes for a person; a
// foreign key keeps its ON UPDATE rule; one drop says "statement".
func TestCompareSchemasConstraintNotesAndSingleDrop(t *testing.T) {
	users := CanonTable{Name: "users", Columns: []CanonColumn{{Name: "id", Type: "int"}}}
	orders := CanonTable{Name: "orders", Columns: []CanonColumn{{Name: "id", Type: "int"}, {Name: "uid", Type: "int"}, {Name: "x"}}}
	srcOrders := orders
	srcOrders.Constraints = []CanonConstraint{{Kind: "UNIQUE", Name: "o_u", Columns: []string{"uid"}}}
	tgtOrders := orders
	tgtOrders.Constraints = []CanonConstraint{{Kind: "CHECK", Name: "o_c", Check: "CHECK (id > 0)"}}
	fkTable := CanonTable{Name: "items", Columns: []CanonColumn{{Name: "oid", Type: "int"}}, Constraints: []CanonConstraint{
		{Kind: "FOREIGN KEY", Name: "items_fk", Columns: []string{"oid"}, RefTable: "orders", RefColumns: []string{"id"}, OnUpdate: "CASCADE", OnDelete: "NO ACTION"},
	}}
	src := CanonSchema{Driver: "postgres", Schema: "a", Label: "prod · app · a\x1b[31m", Tables: []CanonTable{srcOrders, fkTable}}
	tgt := CanonSchema{Driver: "postgres", Schema: "b", Tables: []CanonTable{tgtOrders, users}}
	c := CompareSchemas(src, tgt)
	scriptHas(t, c.Script,
		`-- REVIEW: table "orders": only the source has CONSTRAINT "o_u" UNIQUE ("uid")`,
		`-- REVIEW: table "orders": only the target has CONSTRAINT "o_c" CHECK (id > 0)`,
		`REFERENCES "b"."orders" ("id") ON UPDATE CASCADE;`,
		"-- WARNING: 1 destructive statement.",
		"prod · app · a")
	if strings.Contains(c.Script, "\x1b") || strings.Contains(c.Script, "ON DELETE NO ACTION") {
		t.Errorf("script:\n%s", c.Script)
	}
	// A column only differing in having no type is shown as "(no type)".
	tgt2 := CanonSchema{Driver: "postgres", Schema: "b", Tables: []CanonTable{{Name: "orders", Columns: []CanonColumn{{Name: "id", Type: "int"}, {Name: "uid", Type: "int"}, {Name: "x", Type: "text"}}}}}
	src2 := CanonSchema{Driver: "postgres", Schema: "a", Tables: []CanonTable{orders}}
	scriptHas(t, CompareSchemas(src2, tgt2).Script, `target has text, source has (no type)`)
}

// Two constraints or indexes with the same name sort by their columns,
// so the files stay the same from run to run.
func TestCanonSortTiesByColumns(t *testing.T) {
	tb := CanonTable{Name: "t", Constraints: []CanonConstraint{
		{Kind: "UNIQUE", Name: "u", Columns: []string{"b"}},
		{Kind: "UNIQUE", Name: "u", Columns: []string{"a"}},
	}, Indexes: []CanonIndex{
		{Name: "i", Exprs: []string{"z"}},
		{Name: "i", Exprs: []string{"y"}},
	}}
	s := sortCanon(CanonSchema{Tables: []CanonTable{tb}})
	if s.Tables[0].Constraints[0].Columns[0] != "a" || s.Tables[0].Indexes[0].Exprs[0] != "y" {
		t.Fatalf("%+v", s.Tables[0])
	}
}
