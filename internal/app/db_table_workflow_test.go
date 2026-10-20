package app_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// tableSession is a fake MySQL server with one table of 250 rows. It
// records every statement and honours LIMIT / OFFSET, so a test can tell
// whether a page was asked from the server.
type tableSession struct {
	dbclient.Session
	mu     sync.Mutex
	sql    []string
	limits []int
	args   [][]any
	reqs   []dbclient.IntrospectionRequest
	ro     []string // statements that ran in a read-only session
	rw     []string // statements that ran in the normal session
}

// roTable is the read-only session of a tableSession.
type roTable struct {
	dbclient.Session
	s *tableSession
}

func (r roTable) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	r.s.mu.Lock()
	r.s.ro = append(r.s.ro, sql)
	r.s.mu.Unlock()
	return r.s.run(ctx, sql, o)
}
func (r roTable) Close() error { return nil }

func (s *tableSession) ReadOnlySession() (dbclient.Session, error) { return roTable{s: s}, nil }

const tableRows = 250

var limitOffset = regexp.MustCompile(`LIMIT (\d+)(?: OFFSET (\d+))?`)

func (s *tableSession) Ping(context.Context) error { return nil }
func (s *tableSession) Close() error               { return nil }
func (s *tableSession) Cancel()                    {}
func (s *tableSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	switch {
	case req.Object != "":
		return dbclient.IntrospectionResult{
			Keys:        []string{"id"},
			Columns:     []dbclient.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "email", Type: "text"}, {Name: "plan", Type: "text"}},
			Indexes:     []dbclient.SchemaIndex{{Name: "PRIMARY", Columns: []string{"id"}, Unique: true}, {Name: "plan_idx", Columns: []string{"plan"}}},
			ForeignKeys: []dbclient.SchemaForeignKey{{Name: "org_fk", Column: "org_id", RefTable: "orgs", RefColumn: "id"}},
			DDL:         "CREATE TABLE `users` (\n  `id` bigint NOT NULL,\n  PRIMARY KEY (`id`)\n)",
		}, nil
	case req.Schema != "":
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active_users", Kind: "VIEW"}}}, nil
	case req.Database != "":
		return dbclient.IntrospectionResult{Schemas: []string{"app"}}, nil
	default:
		return dbclient.IntrospectionResult{Databases: []string{"app"}}, nil
	}
}

func (s *tableSession) Run(ctx context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.rw = append(s.rw, sql)
	s.mu.Unlock()
	return s.run(ctx, sql, opt)
}

func (s *tableSession) run(_ context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.sql = append(s.sql, sql)
	s.limits = append(s.limits, opt.MaxRows)
	s.args = append(s.args, opt.Args)
	s.mu.Unlock()
	switch {
	case strings.HasPrefix(sql, "SELECT COUNT(*)"):
		n := tableRows
		if strings.Contains(sql, "WHERE") {
			n = 7
		}
		return dbclient.Result{Columns: []dbclient.Column{{Name: "count"}}, Rows: [][]dbclient.Cell{{{Text: strconv.Itoa(n), Kind: "number"}}}}
	case strings.Contains(sql, "information_schema.columns"):
		return dbclient.Result{Columns: []dbclient.Column{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}, Rows: [][]dbclient.Cell{
			{{Text: "id"}, {Text: "bigint"}, {Text: "NO"}, {Null: true}},
			{{Text: "email"}, {Text: "text"}, {Text: "YES"}, {Null: true}},
			{{Text: "plan"}, {Text: "text"}, {Text: "YES"}, {Text: "'free'"}},
		}}
	case strings.Contains(sql, "information_schema.triggers"):
		return dbclient.Result{}
	case strings.HasPrefix(sql, "SELECT * FROM"):
		mt := limitOffset.FindStringSubmatch(sql)
		if mt == nil {
			return dbclient.Result{Err: fmt.Errorf("ERROR: no limit in %s", sql)}
		}
		limit, _ := strconv.Atoi(mt[1])
		offset, _ := strconv.Atoi(mt[2]) // "": 0
		r := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "plan"}}}
		for i := offset; i < offset+limit && i < tableRows; i++ {
			r.Rows = append(r.Rows, []dbclient.Cell{{Text: strconv.Itoa(i), Kind: "number"}, {Text: fmt.Sprintf("user-%03d@example.com", i)}, {Text: []string{"free", "pro", "team"}[i%3]}})
		}
		return r
	}
	return dbclient.Result{Affected: 1}
}

func (s *tableSession) ran() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sql...)
}
func (s *tableSession) last() string {
	r := s.ran()
	if len(r) == 0 {
		return ""
	}
	return r[len(r)-1]
}
func (s *tableSession) selects() []string {
	var out []string
	for _, q := range s.ran() {
		if strings.HasPrefix(q, "SELECT * FROM") {
			out = append(out, q)
		}
	}
	return out
}

// tableDriver opens the Databases view on the "local" MySQL connection and
// moves to the table `users`.
func tableDriver(t *testing.T, w, h int) (*testkit.Driver, *tableSession) {
	t.Helper()
	s := &tableSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": `{"connections":[{"id":"local","driver":"mysql","host":"localhost","port":3306}]}`,
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("view did not open:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("enter", "down", "enter")
	for _, name := range []string{"Tables", "users", "active_users"} {
		if !d.WaitForText(name, 2*time.Second) {
			t.Fatalf("tree lacks %s:\n%s", name, ansi.Strip(d.Frame()))
		}
	}
	d.Key("down", "down")
	return d, s
}

func TestDBTableDataViewPagesOnTheServer(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	want := "SELECT * FROM `app`.`users` ORDER BY `id` LIMIT 101 OFFSET 0"
	if got := s.selects(); len(got) != 1 || got[0] != want {
		t.Fatalf("first page SQL = %v, want %s", got, want)
	}
	text := dbText(t, d)
	if strings.Contains(text, "user-100@example.com") {
		t.Fatal("first page holds more than 100 rows")
	}
	if !strings.Contains(text, "rows 1-100") || !strings.Contains(text, "more") {
		t.Fatalf("summary lacks the row range:\n%s", text)
	}
	for _, n := range s.limits {
		if n > 101 {
			t.Fatalf("a query asked for %d rows", n)
		}
	}
	d.Key("]")
	dbWaitPage(t, d, "user-100@example.com")
	if got := s.last(); got != "SELECT * FROM `app`.`users` ORDER BY `id` LIMIT 101 OFFSET 100" {
		t.Fatalf("next page SQL = %s", got)
	}
	d.Key("]")
	dbWaitPage(t, d, "user-249@example.com")
	text = dbText(t, d)
	if !strings.Contains(text, "rows 201-250") || strings.Contains(text, "more") {
		t.Fatalf("last page summary wrong:\n%s", text)
	}
	n := len(s.selects())
	d.Key("]")
	d.Settle(300 * time.Millisecond)
	if len(s.selects()) != n {
		t.Fatal("] past the last page asked the server again")
	}
	d.Key("[")
	dbWaitPage(t, d, "user-100@example.com")
	if got := s.last(); !strings.HasSuffix(got, "LIMIT 101 OFFSET 100") {
		t.Fatalf("previous page SQL = %s", got)
	}
}

func TestDBTableSortAndWhereAreServerSide(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	// Sort by plan, then again to flip it.
	d.Key("o", "down", "down", "enter")
	if !d.WaitFor(func() bool { return strings.HasSuffix(s.last(), "ORDER BY `plan`, `id` LIMIT 101 OFFSET 0") }, 2*time.Second) {
		t.Fatalf("sort SQL = %s", s.last())
	}
	d.Key("o", "down", "down", "enter")
	if !d.WaitFor(func() bool { return strings.HasSuffix(s.last(), "ORDER BY `plan` DESC, `id` LIMIT 101 OFFSET 0") }, 2*time.Second) {
		t.Fatalf("flip SQL = %s", s.last())
	}
	// A filter keeps the sort and starts at the first page.
	d.Key("f")
	d.Type("plan = 'pro'")
	d.Key("enter")
	want := "SELECT * FROM `app`.`users` WHERE (plan = 'pro') ORDER BY `plan` DESC, `id` LIMIT 101 OFFSET 0"
	if !d.WaitFor(func() bool { return s.last() == want }, 2*time.Second) {
		t.Fatalf("filter SQL = %s", s.last())
	}
	// An empty filter clears it.
	d.Key("f")
	for i := 0; i < 20; i++ {
		d.Key("backspace")
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { return !strings.Contains(s.last(), "WHERE") }, 2*time.Second) {
		t.Fatalf("clearing the filter sent %s", s.last())
	}
}

func TestDBTableWhereRefusesAnythingButARead(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	before := len(s.ran())
	for _, bad := range []string{"1=1; DROP TABLE users", "1=1 -- x", "1=1 /* x */", "id = 1) OR (1=1"} {
		d.Key("f")
		d.Type(bad)
		d.Key("enter")
		if !d.WaitForText("Filter refused", 2*time.Second) {
			t.Fatalf("%q was not refused:\n%s", bad, ansi.Strip(d.Frame()))
		}
		d.Settle(200 * time.Millisecond)
	}
	if got := s.ran(); len(got) != before {
		t.Fatalf("a refused filter reached the server: %v", got[before:])
	}
}

func TestDBTableCountOnDemand(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	for _, q := range s.ran() {
		if strings.Contains(q, "COUNT") {
			t.Fatal("rows were counted without #")
		}
	}
	if strings.Contains(dbText(t, d), " of 250") {
		t.Fatal("total shown before #")
	}
	d.Key("#")
	dbWaitPage(t, d, "of 250")
	if got := s.last(); got != "SELECT COUNT(*) FROM `app`.`users`" {
		t.Fatalf("count SQL = %s", got)
	}
	// A new filter forgets the old total.
	d.Key("f")
	d.Type("plan = 'pro'")
	d.Key("enter")
	dbWaitPage(t, d, "user-001@example.com")
	if strings.Contains(dbText(t, d), " of 250") {
		t.Fatalf("stale total after a filter:\n%s", dbText(t, d))
	}
}

func TestDBTableTransposedView(t *testing.T) {
	d, _ := tableDriver(t, 80, 24)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("t")
	text := dbText(t, d)
	for _, want := range []string{"Trans", "row 1 of 100", "id", "email", "user-000@example.com", "plan"} {
		if !strings.Contains(text, want) {
			t.Fatalf("transposed view lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "user-001@example.com") {
		t.Fatalf("transposed view shows more than one row:\n%s", text)
	}
	d.Key("]")
	text = dbText(t, d)
	if !strings.Contains(text, "row 2 of 100") || !strings.Contains(text, "user-001@example.com") {
		t.Fatalf("] did not step to the next row:\n%s", text)
	}
	d.Key("[", "[")
	if !strings.Contains(dbText(t, d), "row 1 of 100") {
		t.Fatalf("[ went before the first row:\n%s", dbText(t, d))
	}
	// v on a value opens it; the cursor line is the column.
	d.Key("j", "v")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL Value") }, time.Second) {
		t.Fatalf("value viewer missing: %s", dbBufName(t, d))
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); got != "user-000@example.com" {
		t.Fatalf("value = %q", got)
	}
	d.Key("q", "t")
	if !strings.Contains(dbText(t, d), "user-099@example.com") {
		t.Fatalf("t did not go back to the grid:\n%s", dbText(t, d))
	}
}

func TestDBTableDescribe(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("d")
	dbWaitPage(t, d, "primary key")
	text := dbText(t, d)
	for _, want := range []string{"Kind", "Column", "id", "bigint", "not null", "can be null", "default 'free'", "Index", "plan_idx", "Foreign key", "org_fk", "orgs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("describe lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "PRIMARY ") && strings.Contains(text, "unique · (id)") {
		t.Fatalf("the key index is listed:\n%s", text)
	}
	for _, q := range s.ran() {
		if strings.HasPrefix(q, "SELECT * FROM") {
			t.Fatal("describe read table rows")
		}
	}
}

func TestDBTableDDLIsAReadOnlySQLBuffer(t *testing.T) {
	d, _ := tableDriver(t, 80, 24)
	d.Key("D")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL DDL: app.users") }, 2*time.Second) {
		t.Fatalf("DDL buffer missing: %s\n%s", dbBufName(t, d), ansi.Strip(d.Frame()))
	}
	if got := dbLua(t, d, `return vim.bo.filetype`); got != "sql" {
		t.Fatalf("filetype = %q", got)
	}
	if got := dbLua(t, d, `return tostring(vim.bo.modifiable)`); got != "false" {
		t.Fatalf("DDL buffer is editable")
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); !strings.Contains(got, "CREATE TABLE `users`") {
		t.Fatalf("DDL text = %q", got)
	}
	// A second open reuses the buffer; q leaves it.
	d.Key("q")
	d.Key("f1")
	d.Type("Databases: Show DDL")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL DDL: app.users") }, 2*time.Second) {
		t.Fatal("DDL buffer did not reopen")
	}
}

func TestDBTableCountAndCopyNameOnTheTree(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("#")
	if !d.WaitForText("250 rows", 2*time.Second) {
		t.Fatalf("count toast missing:\n%s", ansi.Strip(d.Frame()))
	}
	if got := s.last(); got != "SELECT COUNT(*) FROM `app`.`users`" {
		t.Fatalf("tree count SQL = %s", got)
	}
	d.Key("y")
	if !d.WaitFor(func() bool { return dbLua(t, d, `return vim.fn.getreg('"')`) == "`app`.`users`" }, 2*time.Second) {
		t.Fatalf("the quoted name was not copied: %q", dbLua(t, d, `return vim.fn.getreg('"')`))
	}
}

func TestDBTableSelectKeyRunsInTheConsole(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("s")
	dbWaitPage(t, d, "user-000@example.com")
	if got := s.selects(); len(got) != 1 || got[0] != "SELECT * FROM `app`.`users` LIMIT 100;" {
		t.Fatalf("s ran %v", got)
	}
	d.Key("gd")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 2*time.Second) {
		t.Fatalf("gd did not return to the console: %s", dbBufName(t, d))
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); !strings.Contains(got, "SELECT * FROM `app`.`users` LIMIT 100;") || !strings.Contains(got, "-- @conn local") {
		t.Fatalf("console text = %q", got)
	}
	if _, err := os.Stat(dbBufName(t, d)); err != nil {
		t.Fatal(err)
	}
}

func TestDBTableViewOnAViewNode(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("down", "down")
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	if got := s.selects(); len(got) != 1 || !strings.Contains(got[0], "`app`.`active_users`") {
		t.Fatalf("view SQL = %v", got)
	}
}

// The same keys on a real SQLite file through the sqlite3 CLI (skipped
// when the CLI is missing): quoting, paging, an empty page that keeps its
// header, describe and the DDL.
func TestDBTableSQLiteEndToEnd(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	file := filepath.Join(t.TempDir(), "shop.db")
	script := `CREATE TABLE "order items" (id INTEGER PRIMARY KEY, name TEXT NOT NULL, qty INTEGER DEFAULT 5);
CREATE INDEX items_name_idx ON "order items"(name);
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<250) INSERT INTO "order items"(id,name) SELECT i, 'item-' || printf('%03d', i) FROM n;`
	if out, err := exec.Command(bin, file, script).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"shop","driver":"sqlite","path":%q}]}`, file),
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("enter")
	if !d.WaitForText("main", 5*time.Second) {
		t.Fatalf("no database row:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "enter")
	if !d.WaitForText("order items", 5*time.Second) {
		t.Fatalf("no table row:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "down", "o")
	dbWaitPage(t, d, "item-001")
	text := dbText(t, d)
	if !strings.Contains(text, `SELECT * FROM "main"."order items" ORDER`) || strings.Contains(text, "item-101") || !strings.Contains(text, "rows 1-100") || !strings.Contains(text, "more") {
		t.Fatalf("first page:\n%s", text)
	}
	d.Key("]", "]")
	dbWaitPage(t, d, "item-250")
	if text = dbText(t, d); !strings.Contains(text, "rows 201-250") || strings.Contains(text, " more") {
		t.Fatalf("last page:\n%s", text)
	}
	// An empty filter result keeps the header.
	d.Key("f")
	d.Type("id > 1000")
	d.Key("enter")
	dbWaitPage(t, d, "no rows")
	if text = dbText(t, d); !strings.Contains(text, "name") || !strings.Contains(text, "qty") {
		t.Fatalf("empty page lost its header:\n%s", text)
	}
	// A write is refused before it reaches SQLite.
	d.Key("f")
	d.Type("1=1; DROP TABLE \"order items\"")
	d.Key("enter")
	if !d.WaitForText("Filter refused", 2*time.Second) {
		t.Fatalf("not refused:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("f")
	for i := 0; i < 20; i++ {
		d.Key("backspace")
	}
	d.Key("enter")
	d.Key("#")
	dbWaitPage(t, d, "of 250")
	// Describe and DDL come from the file's own catalog.
	d.Key("f1")
	d.Type("Databases: Describe Table")
	d.Key("enter")
	dbWaitPage(t, d, "primary key")
	text = dbText(t, d)
	for _, want := range []string{"Column", "id", "INTEGER", "not null", "default 5", "Index", "items_name_idx", "(name)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("describe lacks %q:\n%s", want, text)
		}
	}
	d.Key("f1")
	d.Type("Databases: Show DDL")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "SQL DDL: main.order items") }, 3*time.Second) {
		t.Fatalf("DDL buffer: %s", dbBufName(t, d))
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); !strings.Contains(got, `CREATE TABLE "order items"`) || !strings.Contains(got, "items_name_idx") {
		t.Fatalf("DDL text:\n%s", got)
	}
	// The table is still there with all its rows.
	out, err := exec.Command(bin, file, `SELECT count(*) FROM "order items"`).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "250" {
		t.Fatalf("rows after the session: %s %v", out, err)
	}
}

func TestDBTableServerErrorKeepsTheViewUsable(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	// A condition the server rejects shows its error; f works again.
	d.Key("f")
	d.Type("no_such_column = 1")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(s.last(), "no_such_column") }, 2*time.Second) {
		t.Fatalf("filter did not reach the server: %s", s.last())
	}
	d.Key("f")
	for i := 0; i < 20; i++ {
		d.Key("backspace")
	}
	d.Key("enter")
	dbWaitPage(t, d, "user-000@example.com")
}

func TestDBTableIdleSendsNothing(t *testing.T) {
	d, s := tableDriver(t, 80, 24)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("#")
	dbWaitPage(t, d, "of 250")
	d.Settle(500 * time.Millisecond)
	n := len(s.ran())
	before, _ := d.Updates()
	d.Settle(2 * time.Second)
	for _, msg := range d.UpdatesSince(before) {
		if strings.Contains(msg, "db") || strings.Contains(msg, "Table") {
			t.Fatalf("an idle data view sent %s", msg)
		}
	}
	if len(s.ran()) != n {
		t.Fatalf("an idle data view queried the server: %v", s.ran()[n:])
	}
}

func TestDBTableSelectKeyTwiceUsesTheSameLine(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("s")
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("gd", "esc", "f1")
	d.Type("Databases: Focus")
	d.Key("enter", "s")
	if !d.WaitFor(func() bool { return len(s.selects()) == 2 }, 3*time.Second) {
		t.Fatalf("second s did not run: %v", s.selects())
	}
	d.Settle(300 * time.Millisecond)
	d.Key("gd")
	got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`)
	if n := strings.Count(got, "SELECT * FROM `app`.`users` LIMIT 100;"); n != 1 {
		t.Fatalf("the statement is in the console %d times:\n%s", n, got)
	}
}

func TestDBTableRightClickMenuOpensTheData(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	x, y := -1, -1
	for row, line := range strings.Split(ansi.Strip(d.Frame()), "\n") {
		if i := strings.Index(line, " users"); i >= 0 && !strings.Contains(line, "active_users") && i < 40 {
			x, y = i+2, row
			break
		}
	}
	if y < 0 {
		t.Fatalf("no users row:\n%s", ansi.Strip(d.Frame()))
	}
	d.RightClick(x, y)
	for _, want := range []string{"Open data", "Describe", "Show DDL", "Count rows", "Copy name"} {
		if !d.WaitForText(want, time.Second) {
			t.Fatalf("menu lacks %q:\n%s", want, ansi.Strip(d.Frame()))
		}
	}
	d.Key("enter")
	dbWaitPage(t, d, "user-000@example.com")
	if got := s.selects(); len(got) != 1 {
		t.Fatalf("menu did not open the data once: %v", got)
	}
}

// The page and the count run in a read-only session: the server itself
// refuses a write, so the function deny-list is not the only guard.
func TestDBTablePageAndCountRunReadOnly(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("o")
	dbWaitPage(t, d, "user-000@example.com")
	d.Key("#")
	dbWaitPage(t, d, "of 250")
	d.Key("]")
	dbWaitPage(t, d, "user-100@example.com")
	d.Key("escape")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.rw {
		if strings.HasPrefix(q, "SELECT * FROM") || strings.HasPrefix(q, "SELECT COUNT") {
			t.Errorf("ran on the read-write session: %s", q)
		}
	}
	var pages, counts int
	for _, q := range s.ro {
		if strings.HasPrefix(q, "SELECT * FROM") {
			pages++
		}
		if strings.HasPrefix(q, "SELECT COUNT") {
			counts++
		}
	}
	if pages != 2 || counts != 1 {
		t.Fatalf("read-only runs: %d pages, %d counts (%v)", pages, counts, s.ro)
	}
}
