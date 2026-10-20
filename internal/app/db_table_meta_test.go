package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// metaSession answers the catalog queries of a table with canned rows and
// records what it was asked.
type metaSession struct {
	dbclient.Session
	mu   sync.Mutex
	runs []string
	args [][]any
	reqs []dbclient.IntrospectionRequest
	ir   dbclient.IntrospectionResult
	rows map[string][][]string // SQL substring -> rows
}

func (s *metaSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	return s.ir, nil
}

func (s *metaSession) Run(_ context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, sql)
	s.args = append(s.args, opt.Args)
	for key, rows := range s.rows {
		if strings.Contains(sql, key) {
			r := dbclient.Result{Columns: []dbclient.Column{{Name: "a"}}}
			for _, row := range rows {
				var cells []dbclient.Cell
				for _, v := range row {
					if v == "<null>" {
						cells = append(cells, dbclient.Cell{Null: true})
					} else {
						cells = append(cells, dbclient.Cell{Text: v})
					}
				}
				r.Rows = append(r.Rows, cells)
			}
			return r
		}
	}
	return dbclient.Result{}
}

func pgMetaSession() *metaSession {
	return &metaSession{
		ir: dbclient.IntrospectionResult{
			Keys:    []string{"id"},
			Columns: []dbclient.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "email", Type: "text"}},
			Indexes: []dbclient.SchemaIndex{{Name: "users_pkey", Columns: []string{"id"}, Unique: true}, {Name: "users_email_key", Columns: []string{"email"}, Unique: true}, {Name: "users_plan_idx", Columns: []string{"plan", "id"}}},
			ForeignKeys: []dbclient.SchemaForeignKey{
				{Name: "users_org_fk", Column: "org_id", RefTable: "orgs", RefColumn: "id"},
				{Name: "users_org_fk", Column: "org_part", RefTable: "orgs", RefColumn: "part"},
			},
		},
		rows: map[string][][]string{
			"information_schema.columns":  {{"id", "bigint", "NO", "nextval('users_id_seq'::regclass)"}, {"email", "text", "YES", "<null>"}},
			"information_schema.triggers": {{"users_audit", "AFTER", "INSERT"}},
		},
	}
}

func TestDBDescribeListsColumnsKeysIndexesForeignKeysAndTriggers(t *testing.T) {
	sess := pgMetaSession()
	tbl := dbTable{driver: "postgres", schema: "public", name: "users"}
	meta, err := dbLoadTableMeta(context.Background(), sess, tbl, dbclient.IntrospectionRequest{Schema: "public", Object: "users"})
	if err != nil {
		t.Fatal(err)
	}
	res := dbDescribeResult(tbl, meta)
	var text []string
	for _, row := range res.Rows {
		var cells []string
		for _, c := range row {
			cells = append(cells, c.String())
		}
		text = append(text, strings.Join(cells, "|"))
	}
	joined := strings.Join(text, "\n")
	for _, want := range []string{
		"Column|id|bigint|not null · default nextval('users_id_seq'::regclass) · primary key",
		"Column|email|text|can be null",
		"Index|users_email_key||unique · (email)",
		"Index|users_plan_idx||(plan, id)",
		"Foreign key|users_org_fk||(org_id, org_part) → orgs (id, part)",
		"Trigger|users_audit||AFTER INSERT",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("describe lacks %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "users_pkey") {
		t.Errorf("the key index is listed twice:\n%s", joined)
	}
	if len(res.Columns) != 4 || res.Columns[0].Name != "Kind" {
		t.Fatalf("describe columns: %+v", res.Columns)
	}
	for _, q := range sess.runs {
		if strings.Contains(q, "users") && !strings.Contains(q, "$") {
			t.Errorf("table name spliced into SQL instead of bound: %s", q)
		}
	}
}

func TestDBDDLPostgresIsRebuiltFromTheCatalogs(t *testing.T) {
	sess := pgMetaSession()
	tbl := dbTable{driver: "postgres", schema: "public", name: "us\"ers"}
	meta, err := dbLoadTableMeta(context.Background(), sess, tbl, dbclient.IntrospectionRequest{Schema: "public", Object: tbl.name})
	if err != nil {
		t.Fatal(err)
	}
	ddl := dbBuildDDL(tbl, meta)
	for _, want := range []string{
		"-- Rebuilt from the catalogs",
		`CREATE TABLE "public"."us""ers" (`,
		`  "id" bigint NOT NULL DEFAULT nextval('users_id_seq'::regclass),`,
		`  "email" text,`,
		`  PRIMARY KEY ("id")`,
		`CREATE UNIQUE INDEX "users_email_key" ON "public"."us""ers" ("email");`,
		`CREATE INDEX "users_plan_idx" ON "public"."us""ers" ("plan", "id");`,
		`ALTER TABLE "public"."us""ers" ADD CONSTRAINT "users_org_fk" FOREIGN KEY ("org_id", "org_part") REFERENCES "public"."orgs" ("id", "part");`,
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("DDL lacks %q:\n%s", want, ddl)
		}
	}
	if strings.Contains(ddl, "users_pkey") {
		t.Errorf("DDL repeats the key index:\n%s", ddl)
	}
}

func TestDBDDLUsesTheServerTextForSQLiteAndMySQL(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql"} {
		sess := &metaSession{ir: dbclient.IntrospectionResult{DDL: "CREATE TABLE t (id INTEGER PRIMARY KEY)\x1b[31m"}}
		tbl := dbTable{driver: driver, name: "t"}
		meta, err := dbLoadTableMeta(context.Background(), sess, tbl, dbclient.IntrospectionRequest{Object: "t"})
		if err != nil {
			t.Fatal(err)
		}
		ddl := dbBuildDDL(tbl, meta)
		if !strings.Contains(ddl, "CREATE TABLE t (id INTEGER PRIMARY KEY)") || strings.Contains(ddl, "\x1b") || strings.Contains(ddl, "Rebuilt") {
			t.Errorf("%s DDL = %q", driver, ddl)
		}
	}
}

func TestDBMetaSQLiteQuotesTheTableName(t *testing.T) {
	sess := &metaSession{rows: map[string][][]string{"table_info": {{"0", "id", "INTEGER", "1", "<null>", "1"}}}}
	tbl := dbTable{driver: "sqlite", schema: "main", name: `we"ird'name`}
	if _, err := dbLoadTableMeta(context.Background(), sess, tbl, dbclient.IntrospectionRequest{Object: tbl.name}); err != nil {
		t.Fatal(err)
	}
	var sawInfo, sawTrigger bool
	for _, q := range sess.runs {
		if strings.Contains(q, "table_info") {
			sawInfo = true
			if !strings.Contains(q, `PRAGMA "main".table_info("we""ird'name")`) {
				t.Errorf("table_info not quoted: %s", q)
			}
		}
		if strings.Contains(q, "sqlite_master") {
			sawTrigger = true
			if !strings.Contains(q, `tbl_name = 'we"ird''name'`) {
				t.Errorf("trigger query literal not escaped: %s", q)
			}
		}
	}
	if !sawInfo || !sawTrigger {
		t.Fatalf("queries: %v", sess.runs)
	}
}

func TestDBDescribeListsMySQLCheckConstraints(t *testing.T) {
	sess := &metaSession{rows: map[string][][]string{
		"information_schema.columns":           {{"age", "int", "YES", "<null>"}},
		"information_schema.check_constraints": {{"age_ok", "(`age` >= 0)"}},
	}}
	tbl := dbTable{driver: "mysql", schema: "app", name: "users"}
	meta, err := dbLoadTableMeta(context.Background(), sess, tbl, dbclient.IntrospectionRequest{Schema: "app", Object: "users"})
	if err != nil {
		t.Fatal(err)
	}
	res := dbDescribeResult(tbl, meta)
	found := false
	for _, row := range res.Rows {
		if row[0].Text == "Check" && row[1].Text == "age_ok" && row[3].Text == "(`age` >= 0)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("describe lacks the check constraint: %+v", res.Rows)
	}
}

func TestDBSQLiteRowidName(t *testing.T) {
	for _, c := range []struct {
		cols []string
		ddl  string
		want string
	}{
		{[]string{"a"}, "CREATE TABLE t (a)", "rowid"},
		{[]string{"a", "RowId"}, "CREATE TABLE t (a, RowId)", "_rowid_"},
		{[]string{"rowid", "_rowid_", "oid"}, "CREATE TABLE t (rowid, _rowid_, oid)", ""},
		{[]string{"a"}, "CREATE TABLE t (a) WITHOUT ROWID", ""},
		{[]string{"a"}, "CREATE TABLE t (a) STRICT, WITHOUT ROWID", ""},
		{[]string{"without_rowid"}, "CREATE TABLE t (without_rowid)", "rowid"},
	} {
		if got := dbSQLiteRowid(c.cols, c.ddl); got != c.want {
			t.Errorf("dbSQLiteRowid(%v, %q) = %q, want %q", c.cols, c.ddl, got, c.want)
		}
	}
}
