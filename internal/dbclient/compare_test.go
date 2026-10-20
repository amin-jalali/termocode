package dbclient

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture schemas: the source (the model) and the target (to update).
func compareFixture(driver string) (src, tgt CanonSchema) {
	users := CanonTable{Name: "users", Columns: []CanonColumn{
		{Name: "id", Type: "bigint", NotNull: true},
		{Name: "email", Type: "text", NotNull: true},
		{Name: "plan", Type: "text", Default: "'free'"},
	}, Constraints: []CanonConstraint{
		{Kind: "UNIQUE", Name: "users_email_key", Columns: []string{"email"}},
		{Kind: "PRIMARY KEY", Name: "users_pkey", Columns: []string{"id"}},
	}}
	orders := CanonTable{Name: `odd"name`, Columns: []CanonColumn{{Name: "id", Type: "bigint", NotNull: true}, {Name: "user_id", Type: "bigint"}},
		Constraints: []CanonConstraint{{Kind: "FOREIGN KEY", Name: "orders_user_fk", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}, OnDelete: "CASCADE"}},
		Indexes:     []CanonIndex{{Table: `odd"name`, Name: "orders_user_idx", Exprs: []string{`"user_id"`}}}}
	tgtUsers := users
	tgtUsers.Columns = []CanonColumn{
		{Name: "id", Type: "bigint", NotNull: true},
		{Name: "email", Type: "varchar(100)"},
		{Name: "age", Type: "integer"},
	}
	tgtUsers.Indexes = []CanonIndex{{Table: "users", Name: "users_age_idx", Exprs: []string{`"age"`}}}
	src = CanonSchema{Driver: driver, Schema: "public",
		Tables: []CanonTable{users, orders},
		Views:  []CanonView{{Name: "active_users", Definition: "SELECT id FROM users WHERE plan <> 'free'"}},
	}
	tgt = CanonSchema{Driver: driver, Schema: "staging",
		Tables: []CanonTable{{Name: "old_logs", Columns: []CanonColumn{{Name: "line", Type: "text"}}}, tgtUsers},
		Views:  []CanonView{{Name: "active_users", Definition: "SELECT id FROM users"}},
	}
	return src, tgt
}

func TestCanonFilesAreSortedQuotedAndStable(t *testing.T) {
	src, _ := compareFixture("postgres")
	// The same schema in another order gives the same text.
	shuffled := src
	shuffled.Tables = []CanonTable{src.Tables[1], src.Tables[0]}
	a, b := CanonFiles(src), CanonFiles(shuffled)
	if len(a) != len(b) {
		t.Fatalf("files %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("not stable:\n%s\n---\n%s", a[i].Text, b[i].Text)
		}
	}
	var names []string
	for _, f := range a {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "tables.sql,views.sql,routines.sql,indexes.sql" {
		t.Fatalf("file names %s", got)
	}
	tables := a[0].Text
	for _, want := range []string{
		`CREATE TABLE "odd""name" (`,
		`  "user_id" bigint,`,
		`  CONSTRAINT "orders_user_fk" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE`,
		`CREATE TABLE "users" (`,
		`  "plan" text DEFAULT 'free',`,
		`  CONSTRAINT "users_pkey" PRIMARY KEY ("id"),` + "\n" + `  CONSTRAINT "users_email_key" UNIQUE ("email")`,
	} {
		if !strings.Contains(tables, want) {
			t.Errorf("tables.sql lacks %q:\n%s", want, tables)
		}
	}
	if strings.Index(tables, `"odd""name"`) > strings.Index(tables, `"users" (`) {
		t.Errorf("tables not sorted by name:\n%s", tables)
	}
	if strings.Contains(tables, "public") {
		t.Errorf("schema name leaked into canonical DDL:\n%s", tables)
	}
	if !strings.Contains(a[3].Text, `CREATE INDEX "orders_user_idx" ON "odd""name" ("user_id");`) {
		t.Errorf("indexes.sql:\n%s", a[3].Text)
	}
	// MySQL quotes with backticks.
	my, _ := compareFixture("mysql")
	if f := CanonFiles(my); !strings.Contains(f[0].Text, "CREATE TABLE `users` (") {
		t.Errorf("mysql quoting:\n%s", f[0].Text)
	}
	// SQLite has no routines file.
	sq, _ := compareFixture("sqlite")
	for _, f := range CanonFiles(sq) {
		if f.Name == "routines.sql" {
			t.Errorf("sqlite has a routines file")
		}
	}
}

func TestCompareSchemasScriptMakesTargetMatchSource(t *testing.T) {
	src, tgt := compareFixture("postgres")
	c := CompareSchemas(src, tgt)
	if len(c.Files) != 4 {
		t.Fatalf("files: %+v", c.Files)
	}
	for _, f := range c.Files {
		if f.Name == "routines.sql" && f.Old != f.New {
			t.Errorf("routines differ")
		}
		if f.Name == "tables.sql" && (f.Old == f.New || !strings.Contains(f.Old, "old_logs") || strings.Contains(f.New, "old_logs")) {
			t.Errorf("tables.sql old (target) / new (source) sides wrong:\n%s\n---\n%s", f.Old, f.New)
		}
	}
	s := c.Script
	for _, want := range []string{
		"-- review before running",
		"Nothing in this script has been run",
		`DROP VIEW "staging"."active_users";`,
		`DROP INDEX "staging"."users_age_idx";`,
		`CREATE TABLE "staging"."odd""name" (`,
		`ALTER TABLE "staging"."users" ADD COLUMN "plan" text DEFAULT 'free';`,
		`ALTER TABLE "staging"."users" DROP COLUMN "age";`,
		`DROP TABLE "staging"."old_logs";`,
		`ALTER TABLE "staging"."odd""name" ADD CONSTRAINT "orders_user_fk" FOREIGN KEY ("user_id") REFERENCES "staging"."users" ("id") ON DELETE CASCADE;`,
		`CREATE INDEX "orders_user_idx" ON "staging"."odd""name" ("user_id");`,
		`CREATE VIEW "staging"."active_users" AS`,
		`-- REVIEW: column "users"."email" differs`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
	// The destructive statements are listed at the top and each one is
	// marked where it stands.
	head := s[:strings.Index(s, "\n\n")]
	if !strings.Contains(head, "WARNING") || !strings.Contains(head, `DROP TABLE "staging"."old_logs"`) || !strings.Contains(head, `DROP COLUMN "age"`) {
		t.Errorf("header does not warn about the drops:\n%s", head)
	}
	for _, stmt := range []string{`DROP TABLE "staging"."old_logs";`, `ALTER TABLE "staging"."users" DROP COLUMN "age";`, `DROP VIEW "staging"."active_users";`} {
		i := strings.Index(s, "\n"+stmt)
		if i < 0 || !strings.HasSuffix(s[:i], "-- DESTRUCTIVE: review before running") {
			t.Errorf("%s is not marked destructive:\n%s", stmt, s)
		}
	}
	if c.Destructive != 4 { // view, index, column, table
		t.Errorf("destructive = %d", c.Destructive)
	}
	// Order: views and indexes are dropped before tables change, created after.
	order := []string{`DROP VIEW`, `DROP INDEX`, `CREATE TABLE`, `ADD COLUMN`, `DROP COLUMN`, `DROP TABLE`, `ADD CONSTRAINT`, `CREATE INDEX`, `CREATE VIEW`}
	at := -1
	body := s[len(head):]
	for _, o := range order {
		i := strings.Index(body, o)
		if i < at {
			t.Errorf("%s is out of order (%d < %d):\n%s", o, i, at, s)
		}
		at = i
	}
	// The same schema twice: no statements, a plain note.
	same := CompareSchemas(src, src)
	if same.Changes != 0 || same.Destructive != 0 || strings.Contains(same.Script, "\nDROP") || !strings.Contains(same.Script, "The schemas are the same") {
		t.Errorf("same schemas:\n%s", same.Script)
	}
}

func TestCompareSchemasMySQLAndSQLiteSyntax(t *testing.T) {
	src, tgt := compareFixture("mysql")
	s := CompareSchemas(src, tgt).Script
	for _, want := range []string{"DROP INDEX `users_age_idx` ON `staging`.`users`;", "ALTER TABLE `staging`.`users` DROP COLUMN `age`;", "CREATE INDEX `orders_user_idx` ON `staging`.`odd\"name`"} {
		if !strings.Contains(s, want) {
			t.Errorf("mysql script lacks %q:\n%s", want, s)
		}
	}
	src, tgt = compareFixture("sqlite")
	src.Schema, tgt.Schema = "main", "main"
	s = CompareSchemas(src, tgt).Script
	for _, want := range []string{`DROP INDEX "users_age_idx";`, `ALTER TABLE "users" DROP COLUMN "age";`, `CONSTRAINT "orders_user_fk" FOREIGN KEY`} {
		if !strings.Contains(s, want) {
			t.Errorf("sqlite script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "ADD CONSTRAINT") {
		t.Errorf("sqlite cannot add constraints with ALTER TABLE:\n%s", s)
	}
}

// Server text with escape sequences never reaches the canonical text.
func TestCanonCleansServerText(t *testing.T) {
	src := CanonSchema{Driver: "postgres", Views: []CanonView{{Name: "v", Definition: canonBody(Cell{Text: "SELECT 1\x1b[31m\r\nFROM t  \x07"})}}}
	text := CanonFiles(src)[1].Text
	if strings.ContainsAny(text, "\x1b\x07\r") || !strings.Contains(text, "SELECT 1[31m\nFROM t") {
		t.Fatalf("%q", text)
	}
}

// A real SQLite file through the sqlite3 CLI, read-only.
func TestReadCanonSchemaSQLite(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	path := filepath.Join(t.TempDir(), "a.db")
	script := `CREATE TABLE users (id INTEGER PRIMARY KEY, email text NOT NULL UNIQUE, plan TEXT DEFAULT 'free');
CREATE TABLE "order items" (id INTEGER, user_id INTEGER REFERENCES users(id) ON DELETE CASCADE, PRIMARY KEY (id));
CREATE INDEX items_user ON "order items"(user_id);
CREATE INDEX users_lower ON users(lower(email));
CREATE VIEW paid AS SELECT id FROM users WHERE plan <> 'free';`
	if out, err := exec.Command(bin, path, script).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	s, err := NewSQLiteSession(context.Background(), Conn{ID: "a", Driver: "sqlite", Path: path, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := ReadCanonSchema(context.Background(), s, "sqlite", "main")
	if err != nil {
		t.Fatal(err)
	}
	files := CanonFiles(got)
	all := files[0].Text + files[1].Text + files[2].Text
	for _, want := range []string{
		`CREATE TABLE "order items" (`, `  "user_id" INTEGER,`, `  PRIMARY KEY ("id"),`, `FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE`,
		`  "email" TEXT NOT NULL,`, `  "plan" TEXT DEFAULT 'free'`, `  UNIQUE ("email")`,
		`CREATE INDEX "items_user" ON "order items" ("user_id");`, `CREATE INDEX users_lower ON users(lower(email));`,
		`CREATE VIEW paid AS SELECT id FROM users WHERE plan <> 'free';`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("lacks %q:\n%s", want, all)
		}
	}
	if got.Truncated {
		t.Errorf("truncated")
	}
}

// A catalog answer cut at the row limit marks the schema truncated and
// drops the table it stopped in (it may be incomplete).
type cutSession struct{ Session }

func (cutSession) Run(_ context.Context, sql string, o RunOptions) Result {
	if o.MaxRows <= 0 {
		return Result{Err: errUnbounded}
	}
	if strings.Contains(sql, "pg_attribute a ON a.attrelid = c.oid") {
		return Result{Truncated: true, Rows: [][]Cell{
			{{Text: "a"}, {Text: "id"}, {Text: "int"}, {Text: "true"}, {Null: true}, {Text: ""}},
			{{Text: "b"}, {Text: "id"}, {Text: "int"}, {Text: "true"}, {Null: true}, {Text: ""}},
		}}
	}
	return Result{}
}

var errUnbounded = &boundErr{}

type boundErr struct{}

func (*boundErr) Error() string { return "unbounded catalog read" }

func TestReadCanonSchemaIsBounded(t *testing.T) {
	got, err := ReadCanonSchema(context.Background(), cutSession{}, "postgres", "public")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Tables) != 1 || got.Tables[0].Name != "a" {
		t.Fatalf("%+v", got)
	}
	c := CompareSchemas(got, got)
	if !strings.Contains(c.Script, "only part of") {
		t.Errorf("script does not say the read was cut:\n%s", c.Script)
	}
}

// The MySQL rows: display widths dropped, string defaults quoted the same
// for MySQL 8 and MariaDB, PRIMARY and constraint indexes not repeated.
type mysqlRows struct{ Session }

func (mysqlRows) Run(_ context.Context, sql string, _ RunOptions) Result {
	c := func(s ...string) []Cell {
		out := make([]Cell, len(s))
		for i, v := range s {
			if v == "<null>" {
				out[i] = Cell{Null: true}
			} else {
				out[i] = Cell{Text: v}
			}
		}
		return out
	}
	switch {
	case strings.Contains(sql, "information_schema.columns"):
		return Result{Rows: [][]Cell{
			c("users", "id", "int(11)", "NO", "<null>", "auto_increment", "int"),
			c("users", "plan", "varchar(20)", "YES", "free", "", "varchar"),
			c("users", "note", "varchar(20)", "YES", "'x'", "", "varchar"),
			c("users", "flag", "tinyint(1)", "NO", "0", "", "tinyint"),
			c("users", "at", "timestamp", "YES", "CURRENT_TIMESTAMP", "DEFAULT_GENERATED on update CURRENT_TIMESTAMP", "timestamp"),
		}}
	case strings.Contains(sql, "information_schema.table_constraints"):
		return Result{Rows: [][]Cell{
			c("users", "PRIMARY", "PRIMARY KEY", "id", "<null>", "<null>", "<null>", "<null>"),
			c("users", "plan_u", "UNIQUE", "plan", "<null>", "<null>", "<null>", "<null>"),
		}}
	case strings.Contains(sql, "information_schema.statistics"):
		return Result{Rows: [][]Cell{
			c("users", "PRIMARY", "0", "id", "BTREE", "<null>"),
			c("users", "plan_u", "0", "plan", "BTREE", "<null>"),
			c("users", "note_idx", "1", "note", "BTREE", "5"),
		}}
	}
	return Result{}
}

func TestReadCanonSchemaMySQLRows(t *testing.T) {
	got, err := ReadCanonSchema(context.Background(), mysqlRows{}, "mysql", "app")
	if err != nil {
		t.Fatal(err)
	}
	f := CanonFiles(got)
	for _, want := range []string{"`id` int NOT NULL AUTO_INCREMENT", "`plan` varchar(20) DEFAULT 'free'", "`note` varchar(20) DEFAULT 'x'", "`flag` tinyint(1) NOT NULL DEFAULT 0",
		"`at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP", "  PRIMARY KEY (`id`),", "CONSTRAINT `plan_u` UNIQUE (`plan`)"} {
		if !strings.Contains(f[0].Text, want) {
			t.Errorf("lacks %q:\n%s", want, f[0].Text)
		}
	}
	if idx := f[3].Text; strings.Contains(idx, "PRIMARY") || strings.Contains(idx, "plan_u") || !strings.Contains(idx, "CREATE INDEX `note_idx` ON `users` (`note`(5));") {
		t.Errorf("indexes.sql:\n%s", idx)
	}
}

// Columns in another order (ADD COLUMN appends) are not a difference.
func TestCompareSchemasIgnoresColumnOrder(t *testing.T) {
	a := CanonSchema{Driver: "postgres", Schema: "a", Tables: []CanonTable{{Name: "t", Columns: []CanonColumn{{Name: "x", Type: "int"}, {Name: "y", Type: "int"}, {Name: "z", Type: "int"}}}}}
	b := CanonSchema{Driver: "postgres", Schema: "b", Tables: []CanonTable{{Name: "t", Columns: []CanonColumn{{Name: "z", Type: "int"}, {Name: "x", Type: "int"}, {Name: "y", Type: "int"}}}}}
	c := CompareSchemas(a, b)
	if c.Changes != 0 || c.Files[0].Old != c.Files[0].New {
		t.Fatalf("column order counted: %d\n%s", c.Changes, c.Script)
	}
	if b.Tables[0].Columns[0].Name != "z" {
		t.Fatalf("the caller's schema was reordered")
	}
}

// MySQL and MariaDB CHECK constraints are read; a server without the
// CHECK_CONSTRAINTS table is skipped without an error.
type mysqlChecks struct {
	mysqlRows
	old bool
}

func (m mysqlChecks) Run(ctx context.Context, sql string, o RunOptions) Result {
	if strings.Contains(sql, "information_schema.check_constraints") {
		if m.old {
			return Result{Err: errors.New("Table 'information_schema.CHECK_CONSTRAINTS' doesn't exist")}
		}
		return Result{Rows: [][]Cell{
			{{Text: "users"}, {Text: "flag_ok"}, {Text: "(`flag` in (0,1))"}},
			{{Text: "users"}, {Text: "id_pos"}, {Text: "`id` > 0"}},
		}}
	}
	return m.mysqlRows.Run(ctx, sql, o)
}

func TestReadCanonSchemaMySQLChecks(t *testing.T) {
	got, err := ReadCanonSchema(context.Background(), mysqlChecks{}, "mysql", "app")
	if err != nil {
		t.Fatal(err)
	}
	f := CanonFiles(got)[0].Text
	for _, want := range []string{"CONSTRAINT `flag_ok` CHECK (`flag` in (0,1))", "CONSTRAINT `id_pos` CHECK (`id` > 0)"} {
		if !strings.Contains(f, want) {
			t.Errorf("lacks %q:\n%s", want, f)
		}
	}
	if _, err := ReadCanonSchema(context.Background(), mysqlChecks{old: true}, "mysql", "app"); err != nil {
		t.Fatalf("old server must be skipped quietly: %v", err)
	}
}
