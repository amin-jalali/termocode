package app_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/charmbracelet/x/ansi"
)

// consoleSession is a fake SQL session: 205 rows for a read, an error for
// "broken", a run that waits for its context for "slow".
type consoleSession struct {
	dbclient.Session
	mu     sync.Mutex
	sql    []string
	limits []int
}

func (s *consoleSession) Ping(context.Context) error { return nil }
func (s *consoleSession) Close() error               { return nil }
func (s *consoleSession) Cancel()                    {}
func (s *consoleSession) Introspect(context.Context, dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return dbclient.IntrospectionResult{}, nil
}
func (s *consoleSession) Run(ctx context.Context, sql string, opt dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.sql = append(s.sql, sql)
	s.limits = append(s.limits, opt.MaxRows)
	s.mu.Unlock()
	if strings.Contains(sql, "slow") {
		<-ctx.Done()
		return dbclient.Result{Err: ctx.Err()}
	}
	if strings.Contains(sql, "broken") {
		return dbclient.Result{Err: fmt.Errorf("ERROR: bad query (SQLSTATE 42601)\nLINE 1: SELECT broken\x1b[31m")}
	}
	if first := strings.ToUpper(strings.TrimSpace(dbLastLines(sql))); !strings.HasPrefix(first, "SELECT") {
		return dbclient.Result{Affected: 3}
	}
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}}, Notices: []string{"server\x1b[31m notice\nsecond"}}
	for i := 0; i < 205; i++ {
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: fmt.Sprint(i), Kind: "number"}, {Text: fmt.Sprintf("row-%03d", i)}})
	}
	return r
}

// dbLastLines drops the comment lines before the SQL.
func dbLastLines(sql string) string {
	var keep []string
	for _, l := range strings.Split(sql, "\n") {
		if t := strings.TrimSpace(l); len(keep) == 0 && (t == "" || strings.HasPrefix(t, "--")) {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

func (s *consoleSession) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sql) }
func (s *consoleSession) ran() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sql...)
}

func dbLua(t *testing.T, d *testkit.Driver, src string, args ...any) string {
	t.Helper()
	v, e := d.Model().NvimLua(src, args...)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func dbText(t *testing.T, d *testkit.Driver) string {
	return dbLua(t, d, `local b=vim.g.termocode_db_buf; if not b then return '' end; return table.concat(vim.api.nvim_buf_get_lines(b,0,-1,false),'\n')`)
}
func dbBufName(t *testing.T, d *testkit.Driver) string {
	return dbLua(t, d, `return vim.api.nvim_buf_get_name(0)`)
}

const dbTestConns = `{"connections":[{"id":"local","driver":"mysql","host":"localhost","port":3306},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"environment":"production"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","port":5432,"readOnly":true}]}`

func dbDriverSized(t *testing.T, sql string, w, h int) (*testkit.Driver, *consoleSession) {
	t.Helper()
	s := &consoleSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: map[string]string{"query.sql": sql, ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc")
	return d, s
}
func dbDriver(t *testing.T, sql string) (*testkit.Driver, *consoleSession) {
	return dbDriverSized(t, sql, 80, 24)
}
func dbSend(d *testkit.Driver) { d.Key("alt+ctrl+r") }
func dbWaitPage(t *testing.T, d *testkit.Driver, text string) {
	t.Helper()
	if !d.WaitFor(func() bool { return strings.Contains(dbText(t, d), text) }, 3*time.Second) {
		t.Fatalf("result page lacks %q:\n%s\n%s", text, dbText(t, d), ansi.Strip(d.Frame()))
	}
}

func TestDBConsoleKeysGuardPagingAndViews(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT 1;\nDELETE FROM users;\n")
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	if strings.Contains(dbText(t, d), "row-100") {
		t.Fatal("first page holds more than 100 rows")
	}
	if !strings.Contains(dbText(t, d), "page 1 of 3") {
		t.Fatalf("page count missing:\n%s", dbText(t, d))
	}
	d.Key("]")
	if !strings.Contains(dbText(t, d), "row-100") || strings.Contains(dbText(t, d), "row-000") {
		t.Fatal("next page did not replace rows")
	}
	d.Key("[")
	if !strings.Contains(dbText(t, d), "row-000") {
		t.Fatal("previous page did not come back")
	}
	d.Key("tab")
	if !strings.Contains(dbText(t, d), `"name"`) {
		t.Fatal("JSON view missing")
	}
	d.Key("tab")
	if !strings.Contains(dbText(t, d), "server") || strings.Contains(dbText(t, d), "\x1b") {
		t.Fatal("messages absent or unsafe")
	}
	// gd goes back to the statement; the next line is a write.
	d.Key("gd")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "query.sql") }, time.Second) {
		t.Fatalf("gd did not return to the SQL: %s", dbBufName(t, d))
	}
	d.Key("esc", "g", "g", "down", "down")
	dbSend(d)
	if !d.WaitForText("Run DELETE without WHERE on local?", time.Second) {
		t.Fatalf("guard missing: %s", ansi.Strip(d.Frame()))
	}
	frame := ansi.Strip(d.Frame())
	if !strings.Contains(frame, "DELETE FROM users") || !strings.Contains(frame, "Every row of users will go.") || strings.Contains(frame, "Always") {
		t.Fatalf("wrong guard: %s", frame)
	}
	if s.count() != 1 {
		t.Fatal("write ran before approval")
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if s.count() != 1 {
		t.Fatal("cancel ran the write")
	}
	dbSend(d)
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return s.count() == 2 }, time.Second) {
		t.Fatal("approved write did not run")
	}
	dbWaitPage(t, d, "3 rows changed")
	for _, n := range s.limits {
		if n <= 0 || n > 10000 {
			t.Fatalf("unbounded result: %d", n)
		}
	}
}

func TestDBConsoleGuardProductionAndReadOnly(t *testing.T) {
	for _, tc := range []struct{ conn, title string }{
		{"prod", "Write to production (prod)?"},
		{"ro", "ro is read-only. Run anyway?"},
	} {
		t.Run(tc.conn, func(t *testing.T) {
			d, s := dbDriver(t, "-- @conn "+tc.conn+"\nINSERT INTO t VALUES (1);\n")
			d.Key("down")
			dbSend(d)
			if !d.WaitForText(tc.title, time.Second) {
				t.Fatalf("card missing: %s", ansi.Strip(d.Frame()))
			}
			if f := ansi.Strip(d.Frame()); strings.Contains(f, "Always") || !strings.Contains(f, "INSERT INTO t VALUES (1)") {
				t.Fatalf("card: %s", f)
			}
			d.Key("esc")
			d.Settle(200 * time.Millisecond)
			if s.count() != 0 {
				t.Fatal("write ran without approval")
			}
		})
	}
}

func TestDBConsoleNewAndAttachPicker(t *testing.T) {
	d, _ := dbDriver(t, "SELECT 1;\n")
	dbSend(d)
	if !d.WaitForText("which connection", time.Second) {
		t.Fatalf("project SQL did not ask for a connection: %s", ansi.Strip(d.Frame()))
	}
	d.Key("enter")
	dbWaitPage(t, d, "row-000")
	// The choice is remembered: a second send asks nothing.
	d.Key("gd", "esc")
	dbSend(d)
	d.Settle(300 * time.Millisecond)
	if strings.Contains(ansi.Strip(d.Frame()), "which connection") {
		t.Fatal("attach picker asked twice")
	}
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter", "n")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, time.Second) {
		t.Fatal("n did not open the console")
	}
	if !strings.Contains(dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`), "-- @conn local") {
		t.Fatal("console header absent")
	}
	path := dbBufName(t, d)
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("console file mode: %v %v", st, err)
	}
	if !strings.HasPrefix(path, os.Getenv("XDG_CONFIG_HOME")) {
		t.Fatalf("console outside user config: %s", path)
	}
}

func TestDBConsoleFilterSortValueHistoryAndCancel(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT 1;\nSELECT slow;\n")
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("o", "enter", "o", "enter")
	if !strings.Contains(dbText(t, d), "row-204") {
		t.Fatal("sort did not reverse")
	}
	d.Key("f")
	d.Type("row-123")
	d.Key("enter")
	text := dbText(t, d)
	if !strings.Contains(text, "row-123") || strings.Contains(text, "row-124") {
		t.Fatalf("filter: %s", text)
	}
	// v on the cell under the cursor: the value page; q comes back.
	d.Key("$", "v")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL Value") }, time.Second) {
		t.Fatal("value viewer missing")
	}
	if got := dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`); got != "row-123" {
		t.Fatalf("value page shows %q", got)
	}
	d.Key("q")
	d.Key("$", "v") // a second open reuses the page (no E95)
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL Value") }, time.Second) {
		t.Fatal("value viewer did not reopen")
	}
	d.Key("q", "h")
	if !d.WaitForText("SQL history", time.Second) {
		t.Fatal("history picker absent")
	}
	d.Type("Clear history")
	d.Key("enter")
	if !d.WaitForText("Clear SQL history?", time.Second) {
		t.Fatalf("clear history guard absent: %s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter")
	hist := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db", "history.jsonl")
	if b, _ := os.ReadFile(hist); strings.TrimSpace(string(b)) != "" {
		t.Fatalf("history not cleared: %s", b)
	}
	d.Key("gd", "esc", "G")
	dbSend(d)
	dbWaitPage(t, d, "Running")
	// Esc in the console cancels too; here the page has the focus.
	d.Key("esc")
	dbWaitPage(t, d, "Cancelled")
	count := s.count()
	d.Key("q")
	d.Settle(time.Second)
	before, _ := d.Updates()
	d.Settle(time.Second)
	for _, msg := range d.UpdatesSince(before) {
		if strings.Contains(msg, "db") {
			t.Fatalf("idle page sent %s", msg)
		}
	}
	if s.count() != count {
		t.Fatal("idle console queried the database")
	}
}

func TestDBConsoleHistoryRecordsAndReruns(t *testing.T) {
	ai.RegisterSecret("hunter2-db-secret")
	d, s := dbDriver(t, "-- @conn local\nSELECT 'hunter2-db-secret';\n")
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	if strings.Contains(dbText(t, d), "hunter2-db-secret") {
		t.Fatal("secret on the result page")
	}
	hist := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db", "history.jsonl")
	var b []byte
	if !d.WaitFor(func() bool { b, _ = os.ReadFile(hist); return len(b) > 0 }, time.Second) {
		t.Fatal("history not written")
	}
	if strings.Contains(string(b), "hunter2-db-secret") {
		t.Fatalf("secret in history: %s", b)
	}
	if st, _ := os.Stat(hist); st.Mode().Perm() != 0o600 {
		t.Fatalf("history mode %v", st.Mode())
	}
	d.Key("h")
	if !d.WaitForText("SQL history", time.Second) {
		t.Fatal("history picker absent")
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { return s.count() == 2 }, time.Second) {
		t.Fatal("history entry did not run again")
	}
}

func TestDBConsoleRunFileStopsOnErrorAndShowsIt(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT broken;\nDELETE FROM users;\n")
	d.Key("f1")
	d.Type("Databases: Run File")
	d.Key("enter")
	dbWaitPage(t, d, "ERROR: bad query")
	text := dbText(t, d)
	if strings.Contains(text, "\x1b") || !strings.Contains(text, "LINE 1: SELECT broken") {
		t.Fatalf("error page:\n%s", text)
	}
	d.Settle(300 * time.Millisecond)
	if s.count() != 1 {
		t.Fatal("run file continued after the error")
	}
}

func TestDBConsoleRunFileAsksForEachWrite(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT 1;\nUPDATE t SET a = 1 WHERE id = 2;\nSELECT 2;\n")
	d.Key("f1")
	d.Type("Databases: Run File")
	d.Key("enter")
	if !d.WaitForText("Run this statement on local?", 2*time.Second) {
		t.Fatalf("write in the file was not reviewed: %s", ansi.Strip(d.Frame()))
	}
	if got := s.ran(); len(got) != 1 {
		t.Fatalf("ran before the card: %v", got)
	}
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return s.count() == 3 }, 2*time.Second) {
		t.Fatalf("file did not finish: %v", s.ran())
	}
}

func TestDBConsoleVisualSelectionAndCursor(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT 1; SELECT 2;\nSELECT 3;\n")
	d.Key("down", "V")
	dbSend(d)
	if !d.WaitFor(func() bool { return s.count() == 2 }, time.Second) {
		t.Fatal("visual selection did not run both selected statements")
	}
	if strings.Contains(strings.Join(s.ran(), " "), "SELECT 3") {
		t.Fatal("selection included the next line")
	}
	d.Key("gd", "esc", "G")
	dbSend(d)
	if !d.WaitFor(func() bool { return s.count() == 3 }, time.Second) {
		t.Fatal("cursor statement did not run")
	}
	if got := s.ran()[2]; !strings.Contains(got, "SELECT 3") || strings.Contains(got, "SELECT 2") {
		t.Fatalf("cursor ran %q", got)
	}
}

func TestDBConsoleCtrlEnterOnlyInSQL(t *testing.T) {
	s := &consoleSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Nvim: true, Files: map[string]string{"query.sql": "-- @conn local\nSELECT 1;\n", "notes.txt": "hello\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:notes.txt")
	d.Key("esc", "ctrl+enter")
	d.Settle(300 * time.Millisecond)
	if strings.Contains(ansi.Strip(d.Frame()), "No request yet") || s.count() != 0 {
		t.Fatal("Ctrl+Enter acted outside a SQL or HTTP buffer")
	}
	d.Action("open:query.sql")
	d.Key("esc", "down", "ctrl+enter")
	dbWaitPage(t, d, "row-000")
}

func TestDBConsolePostgresConnectReason(t *testing.T) {
	restore := app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		return nil, fmt.Errorf("failed to connect to `user=app database=app`:\n\t127.0.0.1:5432 (localhost): server error: FATAL: database \"app\" does not exist (SQLSTATE 3D000)\n\t[::1]:5432 (localhost): dial error: connection refused")
	})
	t.Cleanup(restore)
	d := testkit.New(t, testkit.Options{Files: map[string]string{".termocode/databases.json": `{"connections":[{"id":"local","driver":"postgres","host":"localhost","port":5432}]}`}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter", "enter")
	if !d.WaitForText(`database "app" does not exist`, time.Second) {
		t.Fatalf("reason missing: %s", ansi.Strip(d.Frame()))
	}
}

func TestDBConsoleLongGuardShowsWholeStatement(t *testing.T) {
	sql := "-- @conn local\nUPDATE users SET\n"
	for i := 0; i < 24; i++ {
		sql += fmt.Sprintf(" field%d = %d,\n", i, i)
	}
	sql += " last_field = 'review-this';\n"
	d, s := dbDriver(t, sql)
	d.Key("down")
	dbSend(d)
	if !d.WaitForText("without WHERE", time.Second) {
		t.Fatalf("guard missing: %s", ansi.Strip(d.Frame()))
	}
	for i := 0; i < 40; i++ {
		d.Key("down")
	}
	if !d.WaitForText("review-this", time.Second) {
		t.Fatal("guard hides the statement tail")
	}
	if s.count() != 0 {
		t.Fatal("scrolling the guard ran SQL")
	}
}

func TestDBConsoleJSONPageBatchesAndLens(t *testing.T) {
	s := &consoleSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 150, H: 42, Nvim: true, Config: map[string]string{"config.json": `{"setup_done":true,"animations":"off","db_page_rows":1000}`}, Files: map[string]string{"query.sql": "-- @conn local\nSELECT 1;\n\n-- second\nSELECT 2;\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc", "down")
	lens := func() string {
		return dbLua(t, d, `local rows=_G._termocode_sql and _G._termocode_sql.rows[vim.api.nvim_get_current_buf()] or {};return (rows[2] or '')..'|'..(rows[5] or '')..'|'..tostring(rows[1] or rows[4] or '')`)
	}
	if !d.WaitFor(func() bool { return strings.HasPrefix(lens(), "> run Ctrl+Alt+R · local|> run") }, 2*time.Second) {
		t.Fatalf("statement lenses absent: %q", lens())
	}
	if strings.HasSuffix(lens(), "|") == false {
		t.Fatalf("lens on a comment row: %q", lens())
	}
	dbSend(d)
	dbWaitPage(t, d, "row-204")
	d.Key("gd")
	if !d.WaitFor(func() bool { return strings.Contains(lens(), "205 rows") }, 2*time.Second) {
		t.Fatalf("lens lacks the result: %q", lens())
	}
	d.Key("ctrl+alt+r") // back on the page with the result
	dbWaitPage(t, d, "row-204")
	before, _ := d.Updates()
	d.Key("tab")
	if !d.WaitFor(func() bool { return strings.Contains(dbText(t, d), "row-204") }, time.Second) {
		t.Fatal("JSON batches incomplete")
	}
	seen := false
	for _, msg := range d.UpdatesSince(before) {
		if msg == "app.dbAppendMsg" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("a large JSON page was sent without batches")
	}
}

// The full stack against the user's sqlite3 CLI (skipped without it).
func TestDBConsoleRealSQLite(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	if out, err := exec.Command("sqlite3", dbPath, "CREATE TABLE users(id INTEGER PRIMARY KEY, email TEXT); INSERT INTO users(email) VALUES ('ada@example.com'),(NULL),('sam@example.com');").CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	conns := fmt.Sprintf(`{"connections":[{"id":"lite","driver":"sqlite","path":%q}]}`, dbPath)
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{"q.sql": "-- @conn lite\nSELECT id, email FROM users ORDER BY id;\nDELETE FROM users WHERE id = 2;\n", ".termocode/databases.json": conns}})
	d.Action("open:q.sql")
	d.Key("esc", "down")
	dbSend(d)
	dbWaitPage(t, d, "ada@example.com")
	text := dbText(t, d)
	if !strings.Contains(text, "<null>") || !strings.Contains(text, "3 rows") {
		t.Fatalf("sqlite page:\n%s", text)
	}
	d.Key("gd", "esc", "j")
	dbSend(d)
	if !d.WaitForText("Run this statement on lite?", 2*time.Second) {
		t.Fatalf("no guard: %s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "enter")
	dbWaitPage(t, d, "1 row changed")
	out, err := exec.Command("sqlite3", dbPath, "SELECT count(*) FROM users;").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "2" {
		t.Fatalf("delete did not reach the file: %v %s", err, out)
	}
}
