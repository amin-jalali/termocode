package app_test

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// cmpSession is a fake PostgreSQL / MySQL server for schema compare. It
// answers the schema list and the column catalog, and records which
// statements ran on the read-only session and which on the normal one.
type cmpSession struct {
	dbclient.Session
	id   string
	cols [][]string // table, column, type, notnull
	mu   *sync.Mutex
	ro   *[]string
	rw   *[]string
}

type cmpRO struct{ s *cmpSession }

func (r cmpRO) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	r.s.mu.Lock()
	*r.s.ro = append(*r.s.ro, r.s.id+": "+sql)
	r.s.mu.Unlock()
	return r.s.answer(sql)
}
func (cmpRO) Introspect(context.Context, dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return dbclient.IntrospectionResult{}, fmt.Errorf("introspect on the read-only session is not used")
}
func (cmpRO) ReadOnlySession() (dbclient.Session, error) { return nil, fmt.Errorf("nested") }
func (cmpRO) Ping(context.Context) error                 { return nil }
func (cmpRO) Close() error                               { return nil }
func (cmpRO) Cancel()                                    {}
func (cmpRO) Begin(context.Context) error                { return fmt.Errorf("read-only") }
func (cmpRO) Commit(context.Context) error               { return nil }
func (cmpRO) Rollback(context.Context) error             { return nil }

func (s *cmpSession) ReadOnlySession() (dbclient.Session, error) { return cmpRO{s: s}, nil }
func (s *cmpSession) Ping(context.Context) error                 { return nil }
func (s *cmpSession) Close() error                               { return nil }
func (s *cmpSession) Cancel()                                    {}
func (s *cmpSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		return dbclient.IntrospectionResult{Databases: []string{"app"}}, nil
	}
	return dbclient.IntrospectionResult{Schemas: []string{"public"}}, nil
}
func (s *cmpSession) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	*s.rw = append(*s.rw, s.id+": "+sql)
	s.mu.Unlock()
	return s.answer(sql)
}

func (s *cmpSession) answer(sql string) dbclient.Result {
	cells := func(v ...string) []dbclient.Cell {
		out := make([]dbclient.Cell, len(v))
		for i, x := range v {
			out[i] = dbclient.Cell{Text: x}
		}
		return out
	}
	switch {
	case strings.Contains(sql, "current_database()"):
		return dbclient.Result{Rows: [][]dbclient.Cell{cells("app")}}
	case strings.Contains(sql, "pg_namespace WHERE nspname NOT LIKE"), strings.Contains(sql, "information_schema.schemata"):
		return dbclient.Result{Rows: [][]dbclient.Cell{cells("public")}}
	case strings.Contains(sql, "pg_attribute a ON a.attrelid = c.oid"), strings.Contains(sql, "c.column_type, c.is_nullable"):
		var rows [][]dbclient.Cell
		for _, c := range s.cols {
			row := cells(c...)
			row = append(row, dbclient.Cell{Null: true}, dbclient.Cell{Text: ""}, dbclient.Cell{Text: c[2]})
			rows = append(rows, row)
		}
		return dbclient.Result{Rows: rows}
	}
	return dbclient.Result{}
}

// cmpDriver opens the Databases view with the given connections (all
// served by fake sessions) and connects each one.
func cmpDriver(t *testing.T, w, h int, conns string, sessions map[string]*cmpSession) (*testkit.Driver, *[]string, *[]string) {
	t.Helper()
	mu := &sync.Mutex{}
	ro, rw := &[]string{}, &[]string{}
	for id, s := range sessions {
		s.id, s.mu, s.ro, s.rw = id, mu, ro, rw
	}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		if s := sessions[c.ID]; s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("no fake for %s", c.ID)
	}))
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: map[string]string{"README.md": "x\n", ".termocode/databases.json": conns}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("view did not open:\n%s", ansi.Strip(d.Frame()))
	}
	return d, ro, rw
}

func cmpOpen(t *testing.T, d *testkit.Driver) {
	t.Helper()
	d.Key("f1")
	d.Type("Databases: Compare Schemas")
	d.Key("enter")
}

func cmpPick(t *testing.T, d *testkit.Driver, step, filter string) {
	t.Helper()
	if !d.WaitForText(step, 3*time.Second) {
		t.Fatalf("no %q picker:\n%s", step, ansi.Strip(d.Frame()))
	}
	d.Type(filter)
	d.Key("enter")
}

const cmpPGConns = `{"connections":[{"id":"dev","driver":"postgres","host":"dev.example","port":5432},{"id":"stage","driver":"postgres","host":"stage.example","port":5432}]}`

// Two PostgreSQL connections: pick source and target, the card shows the
// difference per object kind, and every catalog read ran on a read-only
// session (none on the user's own session, and nothing but reads).
func TestDBCompareSchemasCatalogReadsAreReadOnly(t *testing.T) {
	dev := &cmpSession{cols: [][]string{{"users", "id", "bigint", "true"}, {"users", "email", "text", "true"}}}
	stage := &cmpSession{cols: [][]string{{"users", "id", "bigint", "true"}}}
	d, ro, rw := cmpDriver(t, 120, 36, cmpPGConns, map[string]*cmpSession{"dev": dev, "stage": stage})
	d.Key("enter") // connect dev
	if !d.WaitForText("app", 3*time.Second) {
		t.Fatalf("dev did not connect:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "down", "enter") // connect stage
	d.Settle(300 * time.Millisecond)
	before := len(*rw)
	cmpOpen(t, d)
	cmpPick(t, d, "SOURCE (the one to copy)", "dev")
	cmpPick(t, d, "TARGET (to change)", "stage")
	if !d.WaitForText("tables.sql", 5*time.Second) {
		t.Fatalf("no compare card:\n%s", ansi.Strip(d.Frame()))
	}
	frame := ansi.Strip(d.Frame())
	for _, want := range []string{"Compare schemas", "target: stage · app · public", "source: dev · app · public", `"email" text NOT NULL`} {
		if !strings.Contains(frame, want) {
			t.Errorf("card lacks %q:\n%s", want, frame)
		}
	}
	d.Key("esc")
	if !d.WaitFor(func() bool { return !strings.Contains(ansi.Strip(d.Frame()), "Compare schemas") }, time.Second) {
		t.Errorf("esc did not close the card:\n%s", ansi.Strip(d.Frame()))
	}
	if extra := (*rw)[before:]; len(extra) > 0 {
		t.Errorf("compare ran on the user's session: %q", extra)
	}
	var catalog int
	for _, q := range *ro {
		_, sql, _ := strings.Cut(q, ": ")
		if k, _ := dbclient.Classify(sql, "postgres"); k != dbclient.KindRead {
			t.Errorf("not a read: %s", q)
		}
		if strings.Contains(sql, "pg_attribute a ON a.attrelid = c.oid") {
			catalog++
		}
	}
	if catalog != 2 {
		t.Errorf("column catalog read %d times on read-only sessions, want 2: %q", catalog, *ro)
	}
}

// Schemas of two database types cannot be compared: the user is told
// why, nothing is read.
func TestDBCompareSchemasRefusesTwoDatabaseTypes(t *testing.T) {
	conns := `{"connections":[{"id":"dev","driver":"postgres","host":"dev.example","port":5432},{"id":"shop","driver":"mysql","host":"shop.example","port":3306}]}`
	d, ro, _ := cmpDriver(t, 100, 30, conns, map[string]*cmpSession{"dev": {}, "shop": {}})
	d.Key("enter")
	if !d.WaitForText("app", 3*time.Second) {
		t.Fatalf("dev did not connect:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "down", "enter")
	d.Settle(300 * time.Millisecond)
	cmpOpen(t, d)
	cmpPick(t, d, "SOURCE (the one to copy)", "dev")
	if !d.WaitForText("other type", 2*time.Second) {
		t.Fatalf("target picker does not mark the other type:\n%s", ansi.Strip(d.Frame()))
	}
	n := len(*ro)
	d.Type("shop")
	d.Key("enter")
	if !d.WaitForText("Cannot compare PostgreSQL with MySQL", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", ansi.Strip(d.Frame()))
	}
	if len(*ro) != n {
		t.Errorf("schemas were read after the refusal: %q", (*ro)[n:])
	}
}

// Without a connected SQL database the command says what to do.
func TestDBCompareSchemasNeedsAConnection(t *testing.T) {
	d, _, _ := cmpDriver(t, 100, 30, cmpPGConns, map[string]*cmpSession{"dev": {}, "stage": {}})
	cmpOpen(t, d)
	if !d.WaitForText("No SQL database is connected", 2*time.Second) {
		t.Fatalf("no hint:\n%s", ansi.Strip(d.Frame()))
	}
}

// Two real SQLite files through the sqlite3 CLI: the card, Copy migration
// script (y) and Open script (o). The script is never run: both files
// keep their schema.
func TestDBCompareSchemasSQLiteEndToEnd(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	dir := t.TempDir()
	src, tgt := filepath.Join(dir, "model.db"), filepath.Join(dir, "live.db")
	for path, script := range map[string]string{
		src: `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, plan TEXT DEFAULT 'free');
CREATE INDEX users_email ON users(email);
CREATE VIEW paid AS SELECT id FROM users WHERE plan <> 'free';`,
		tgt: `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, age INTEGER);
CREATE TABLE old_logs (line TEXT);
INSERT INTO old_logs VALUES ('keep me');`,
	} {
		if out, err := exec.Command(bin, path, script).CombinedOutput(); err != nil {
			t.Fatalf("sqlite3: %v\n%s", err, out)
		}
	}
	schema := func(p string) string {
		out, err := exec.Command(bin, p, ".schema").CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		return string(out)
	}
	srcBefore, tgtBefore := schema(src), schema(tgt)
	var mu sync.Mutex
	var clip []string
	t.Cleanup(app.SetClipboardSink(func(s string) { mu.Lock(); clip = append(clip, s); mu.Unlock() }))
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"model","driver":"sqlite","path":%q},{"id":"live","driver":"sqlite","path":%q}]}`, src, tgt),
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("enter")
	if !d.WaitForText("main", 5*time.Second) {
		t.Fatalf("model did not connect:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "down", "enter")
	if !d.WaitFor(func() bool { return strings.Count(ansi.Strip(d.Frame()), "main") >= 2 }, 5*time.Second) {
		t.Fatalf("live did not connect:\n%s", ansi.Strip(d.Frame()))
	}
	cmpOpen(t, d)
	cmpPick(t, d, "SOURCE (the one to copy)", "model")
	cmpPick(t, d, "TARGET (to change)", "live")
	if !d.WaitForText("tables.sql", 5*time.Second) {
		t.Fatalf("no compare card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("y")
	var script string
	if !d.WaitFor(func() bool {
		mu.Lock()
		defer mu.Unlock()
		n := len(clip)
		script = ""
		if n > 0 {
			script = clip[n-1]
		}
		return script != ""
	}, 2*time.Second) {
		t.Fatalf("y copied nothing:\n%s", ansi.Strip(d.Frame()))
	}
	for _, want := range []string{"-- review before running", "-- WARNING: 2 destructive statements", "-- DESTRUCTIVE: review before running\nDROP TABLE \"old_logs\";",
		`ALTER TABLE "users" ADD COLUMN "plan" TEXT DEFAULT 'free';`, `ALTER TABLE "users" DROP COLUMN "age";`, `CREATE INDEX "users_email" ON "users" ("email");`, "CREATE VIEW paid AS"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
	d.Key("o")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "SQL Migration") }, 3*time.Second) {
		t.Fatalf("o did not open the script: %s\n%s", dbBufName(t, d), ansi.Strip(d.Frame()))
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); strings.TrimSpace(got) != strings.TrimSpace(script) {
		t.Errorf("buffer is not the script:\n%s", got)
	}
	if strings.Contains(ansi.Strip(d.Frame()), "tables.sql") {
		t.Errorf("card still open over the script")
	}
	d.Settle(300 * time.Millisecond)
	if schema(src) != srcBefore || schema(tgt) != tgtBefore {
		t.Fatalf("a schema changed:\n%s\n%s", schema(src), schema(tgt))
	}
	if out, _ := exec.Command(bin, tgt, "SELECT line FROM old_logs").CombinedOutput(); strings.TrimSpace(string(out)) != "keep me" {
		t.Fatalf("data changed: %s", out)
	}
}
