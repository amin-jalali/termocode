package app_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/charmbracelet/x/ansi"
)

// planSession is a fake SQL session that answers every explain form and
// records what ran where: "rw" is the connection's own session, "ro" its
// read-only twin.
type planSession struct {
	dbclient.Session
	mu       sync.Mutex
	log      []string
	failJSON bool   // MySQL without FORMAT=JSON
	planJSON string // the PostgreSQL plan to answer ("": pgPlanJSON)
	failPlan error  // every explain fails with this
}

func (s *planSession) add(e string) { s.mu.Lock(); s.log = append(s.log, e); s.mu.Unlock() }
func (s *planSession) events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}
func (s *planSession) has(prefix string) bool {
	for _, e := range s.events() {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}
func (s *planSession) Ping(context.Context) error { return nil }
func (s *planSession) Close() error               { return nil }
func (s *planSession) Cancel()                    {}
func (s *planSession) Introspect(context.Context, dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return dbclient.IntrospectionResult{}, nil
}
func (s *planSession) Begin(context.Context) error    { s.add("rw begin"); return nil }
func (s *planSession) Commit(context.Context) error   { s.add("rw commit"); return nil }
func (s *planSession) Rollback(context.Context) error { s.add("rw rollback"); return nil }
func (s *planSession) ReadOnlySession() (dbclient.Session, error) {
	return &planRO{planSession: s}, nil
}
func (s *planSession) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	s.add("rw run " + sql)
	return s.answer(sql)
}

type planRO struct{ *planSession }

func (r *planRO) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	r.add("ro run " + sql)
	return r.answer(sql)
}
func (r *planRO) Close() error { return nil }

const pgPlanJSON = `[{"Plan":{"Node Type":"Hash Join","Startup Cost":10.5,"Total Cost":1000,"Plan Rows":50,"Hash Cond":"(o.user_id = u.id)","Plans":[` +
	`{"Node Type":"Seq Scan","Relation Name":"orders","Alias":"o","Startup Cost":0,"Total Cost":900,"Plan Rows":50000,"Filter":"(total > 10)\u001b[31m"},` +
	`{"Node Type":"Index Scan","Relation Name":"users","Index Name":"users_pkey","Startup Cost":0.1,"Total Cost":7,"Plan Rows":100}]},` +
	`"Planning Time":0.25}]`

func (s *planSession) answer(sql string) dbclient.Result {
	one := func(col, text string) dbclient.Result {
		return dbclient.Result{Columns: []dbclient.Column{{Name: col}}, Rows: [][]dbclient.Cell{{{Text: text}}}, Elapsed: 4 * time.Millisecond}
	}
	if s.failPlan != nil && strings.Contains(sql, "EXPLAIN") {
		return dbclient.Result{Err: s.failPlan}
	}
	switch {
	case strings.HasPrefix(sql, "EXPLAIN (FORMAT JSON)"), strings.HasPrefix(sql, "EXPLAIN (ANALYZE, FORMAT JSON)"):
		if s.planJSON != "" {
			return one("QUERY PLAN", s.planJSON)
		}
		return one("QUERY PLAN", pgPlanJSON)
	case strings.HasPrefix(sql, "EXPLAIN FORMAT=JSON"):
		if s.failJSON {
			return dbclient.Result{Err: errors.New("Error 1064: You have an error in your SQL syntax near 'FORMAT=JSON'")}
		}
		return one("EXPLAIN", `{"query_block":{"cost_info":{"query_cost":"120.5"},"nested_loop":[{"table":{"table_name":"orders","access_type":"ALL","rows_examined_per_scan":9000,"cost_info":{"read_cost":"100.0","eval_cost":"10.0"}}},{"table":{"table_name":"users","access_type":"eq_ref","key":"PRIMARY","rows_examined_per_scan":1,"cost_info":{"read_cost":"0.5","eval_cost":"0.1"}}}]}}`)
	case strings.HasPrefix(sql, "EXPLAIN ANALYZE"):
		return one("EXPLAIN", "-> Table scan on orders  (cost=1 rows=2) (actual time=0.1..0.2 rows=2 loops=1)")
	case strings.HasPrefix(sql, "EXPLAIN QUERY PLAN"):
		return dbclient.Result{Columns: []dbclient.Column{{Name: "plan"}}, Rows: [][]dbclient.Cell{{{Text: "QUERY PLAN"}}, {{Text: "|--SCAN orders"}}, {{Text: "`--SEARCH users USING INTEGER PRIMARY KEY (rowid=?)"}}}}
	case strings.HasPrefix(sql, "EXPLAIN "):
		r := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "select_type"}, {Name: "table"}, {Name: "type"}, {Name: "key"}, {Name: "rows"}, {Name: "Extra"}}}
		r.Rows = [][]dbclient.Cell{
			{{Text: "1"}, {Text: "SIMPLE"}, {Text: "orders"}, {Text: "ALL"}, {Null: true}, {Text: "9000"}, {Text: "Using where"}},
			{{Text: "1"}, {Text: "SIMPLE"}, {Text: "users"}, {Text: "eq_ref"}, {Text: "PRIMARY"}, {Text: "1"}, {Null: true}},
		}
		return r
	}
	return dbclient.Result{Affected: 3}
}

const planConns = `{"connections":[{"id":"local","driver":"mysql","host":"localhost","port":3306},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"environment":"production"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","port":5432,"readOnly":true},` +
	`{"id":"pg","driver":"postgres","host":"pg.example","port":5432}]}`

func planDriver(t *testing.T, sql string, w, h int) (*testkit.Driver, *planSession) {
	t.Helper()
	s := &planSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: map[string]string{"query.sql": sql, ".termocode/databases.json": planConns}})
	d.Action("open:query.sql")
	d.Key("esc", "down")
	return d, s
}

func planFrame(d *testkit.Driver) string { return ansi.Strip(d.Frame()) }

// explainKey presses the console key of Explain (x) or Explain Analyze (X).
func explainKey(d *testkit.Driver, key string) { d.Key("\\", key) }

func TestExplainPlainShowsPlanViewInAReadOnlySession(t *testing.T) {
	for _, tc := range []struct{ conn, form, want string }{
		{"pg", "EXPLAIN (FORMAT JSON) SELECT * FROM orders o JOIN users u ON u.id = o.user_id", "Seq Scan on orders o"},
		{"local", "EXPLAIN FORMAT=JSON SELECT * FROM orders o JOIN users u ON u.id = o.user_id", "orders  ALL rows=9000"},
	} {
		t.Run(tc.conn, func(t *testing.T) {
			d, s := planDriver(t, "-- @conn "+tc.conn+"\nSELECT * FROM orders o JOIN users u ON u.id = o.user_id;\n", 100, 30)
			explainKey(d, "x")
			dbWaitPage(t, d, tc.want)
			if !s.has("ro run " + tc.form) {
				t.Fatalf("no explain in the read-only session: %q", s.events())
			}
			for _, e := range s.events() {
				if strings.HasPrefix(e, "rw") {
					t.Fatalf("plain explain touched the main session: %q", s.events())
				}
			}
			text := dbText(t, d)
			if !strings.Contains(text, "<- costly") || strings.Count(text, "<- costly") != 1 {
				t.Fatalf("want one costly mark:\n%s", text)
			}
			if strings.Contains(text, "\x1b") {
				t.Fatal("server escape on the page")
			}
			if strings.Contains(planFrame(d), "runs the statement") {
				t.Fatal("plain explain asked a card")
			}
		})
	}
}

func TestExplainPlanViewHasIndentFoldsAndChips(t *testing.T) {
	d, _ := planDriver(t, "-- @conn pg\nSELECT * FROM orders o JOIN users u ON u.id = o.user_id;\n", 80, 24)
	explainKey(d, "x")
	dbWaitPage(t, d, "Hash Join")
	if got := dbLua(t, d, `return vim.wo[vim.fn.bufwinid(vim.g.termocode_db_buf)].foldmethod`); got != "expr" {
		t.Fatalf("foldmethod = %q", got)
	}
	text := dbText(t, d)
	if !strings.Contains(text, "Plan") || !strings.Contains(text, "Planning time: 0.25 ms") || !strings.Contains(text, "\n! ") {
		t.Fatalf("page:\n%s", text)
	}
	// zc on the root folds its children away.
	d.Key("gg")
	lua := `local b=vim.g.termocode_db_buf; local w=vim.fn.bufwinid(b); vim.api.nvim_set_current_win(w);
	  for i,l in ipairs(vim.api.nvim_buf_get_lines(b,0,-1,false)) do if l:find('Hash Join',1,true) then vim.api.nvim_win_set_cursor(w,{i,0}); return tostring(i) end end return '0'`
	dbLua(t, d, lua)
	d.Key("z", "c")
	closed := dbLua(t, d, `local w=vim.fn.bufwinid(vim.g.termocode_db_buf); return tostring(vim.api.nvim_win_call(w,function() return vim.fn.foldclosed(vim.fn.line('.')) end))`)
	if closed == "-1" || closed == "0" {
		t.Fatalf("zc did not fold the root (foldclosed=%s)", closed)
	}
	if f := planFrame(d); strings.Contains(f, "Seq Scan on orders") {
		t.Fatalf("folded plan still shows the children:\n%s", f)
	}
	// 80x24: the frame keeps the whole page width.
	for _, l := range strings.Split(planFrame(d), "\n") {
		if ansi.StringWidth(l) > 80 {
			t.Fatalf("line wider than 80: %q", l)
		}
	}
}

func TestExplainTabularFallbackForMySQLWithoutJSON(t *testing.T) {
	d, s := planDriver(t, "-- @conn local\nSELECT * FROM orders;\n", 100, 30)
	s.failJSON = true
	explainKey(d, "x")
	dbWaitPage(t, d, "orders  ALL rows=9000")
	if !s.has("ro run EXPLAIN SELECT * FROM orders") {
		t.Fatalf("no tabular form: %q", s.events())
	}
}

func TestExplainAnalyzeAsksFirstAndRunsReadsInAReadOnlySession(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nSELECT * FROM orders;\n", 100, 30)
	explainKey(d, "X")
	if !d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second) {
		t.Fatalf("no card:\n%s", planFrame(d))
	}
	f := planFrame(d)
	if !strings.Contains(f, "SELECT * FROM orders") || !strings.Contains(f, "Run") || !strings.Contains(f, "Cancel") || strings.Contains(f, "Always") {
		t.Fatalf("card:\n%s", f)
	}
	if len(s.events()) != 0 {
		t.Fatalf("ran before the card was answered: %q", s.events())
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if len(s.events()) != 0 {
		t.Fatalf("Cancel ran it: %q", s.events())
	}
	explainKey(d, "X")
	d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second)
	d.Key("right", "enter")
	dbWaitPage(t, d, "Seq Scan on orders")
	if !s.has("ro run EXPLAIN (ANALYZE, FORMAT JSON) SELECT * FROM orders") || s.has("rw") {
		t.Fatalf("a read analyzes in the read-only session: %q", s.events())
	}
	if !strings.Contains(dbText(t, d), "Analyze") {
		t.Fatalf("page does not say it analyzed:\n%s", dbText(t, d))
	}
}

func TestExplainAnalyzeOfAWriteIsWrappedAndRolledBack(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nDELETE FROM orders WHERE id = 1;\n", 100, 30)
	explainKey(d, "X")
	if !d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second) {
		t.Fatalf("no card:\n%s", planFrame(d))
	}
	f := planFrame(d)
	if !strings.Contains(f, "rolled back") || !strings.Contains(f, "DELETE FROM orders") {
		t.Fatalf("the card must say the write is rolled back:\n%s", f)
	}
	d.Key("right", "enter")
	dbWaitPage(t, d, "Hash Join")
	got := strings.Join(s.events(), "\n")
	want := "rw begin\nrw run EXPLAIN (ANALYZE, FORMAT JSON) DELETE FROM orders WHERE id = 1\nrw rollback"
	if got != want {
		t.Fatalf("events:\n%s\nwant:\n%s", got, want)
	}
}

func TestExplainAnalyzeProductionHasTheStrongCard(t *testing.T) {
	d, s := planDriver(t, "-- @conn prod\nSELECT * FROM orders;\n", 100, 30)
	explainKey(d, "X")
	if !d.WaitForText("production (prod)", 2*time.Second) {
		t.Fatalf("no production card:\n%s", planFrame(d))
	}
	if f := planFrame(d); !strings.Contains(f, "runs the statement") || strings.Contains(f, "Always") {
		t.Fatalf("card:\n%s", f)
	}
	if len(s.events()) != 0 {
		t.Fatal("ran before approval")
	}
}

func TestExplainAnalyzeOnAReadOnlyConnectionOnlyAllowsReads(t *testing.T) {
	d, s := planDriver(t, "-- @conn ro\nDELETE FROM orders;\nSELECT 1;\n", 100, 30)
	explainKey(d, "X")
	if !d.WaitForText("read-only", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", planFrame(d))
	}
	d.Settle(200 * time.Millisecond)
	if strings.Contains(planFrame(d), "Run") && strings.Contains(planFrame(d), "Cancel") {
		t.Fatalf("a card offered the write:\n%s", planFrame(d))
	}
	if len(s.events()) != 0 {
		t.Fatalf("a write analyze ran on a read-only connection: %q", s.events())
	}
	d.Key("down", "down")
	explainKey(d, "X")
	if !d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second) {
		t.Fatalf("a read must still be allowed:\n%s", planFrame(d))
	}
}

func TestExplainAnalyzeRefusesDDLAndSQLiteHasNone(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nDROP TABLE orders;\n", 100, 30)
	explainKey(d, "X")
	if !d.WaitForText("this cannot be rolled back", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", planFrame(d))
	}
	if len(s.events()) != 0 {
		t.Fatalf("ran: %q", s.events())
	}
}

func TestExplainKeysOnTheResultPage(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nSELECT 1 FROM orders;\n", 100, 30)
	dbSend(d)
	dbWaitPage(t, d, "3 rows changed") // the fake answers a plain run with Affected
	d.Key("x")
	dbWaitPage(t, d, "Seq Scan on orders")
	if !s.has("ro run EXPLAIN (FORMAT JSON) SELECT 1 FROM orders") {
		t.Fatalf("x on the page did not explain its statement: %q", s.events())
	}
	d.Key("X")
	if !d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second) {
		t.Fatalf("X on the page needs the card:\n%s", planFrame(d))
	}
	d.Key("esc", "r")
	d.Settle(300 * time.Millisecond)
	n := 0
	for _, e := range s.events() {
		if strings.HasPrefix(e, "ro run EXPLAIN (FORMAT JSON)") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("r on a plan must plan again (never analyze unasked): %q", s.events())
	}
}

func TestExplainPaletteEntries(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nSELECT 1 FROM orders;\n", 100, 30)
	d.Key("f1")
	d.Type("Databases: Explain Statement")
	d.Key("enter")
	dbWaitPage(t, d, "Seq Scan on orders")
	if !s.has("ro run EXPLAIN (FORMAT JSON)") {
		t.Fatalf("%q", s.events())
	}
}

// The full stack against the user's sqlite3 CLI (skipped without it): x is
// the real query plan, X says SQLite has no ANALYZE and never runs the
// statement.
func TestExplainRealSQLite(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	dbPath := filepath.Join(t.TempDir(), "app.db")
	if out, err := exec.Command("sqlite3", dbPath, "CREATE TABLE orders(id INTEGER PRIMARY KEY, user_id INT, total INT); CREATE TABLE users(id INTEGER PRIMARY KEY, email TEXT); INSERT INTO orders VALUES (1,1,5);").CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	conns := fmt.Sprintf(`{"connections":[{"id":"lite","driver":"sqlite","path":%q}]}`, dbPath)
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{"q.sql": "-- @conn lite\nDELETE FROM orders WHERE total > 1;\n", ".termocode/databases.json": conns}})
	d.Action("open:q.sql")
	d.Key("esc", "down")
	explainKey(d, "x")
	dbWaitPage(t, d, "SCAN orders")
	if !strings.Contains(dbText(t, d), "<- costly") {
		t.Fatalf("a full table scan is the costly node:\n%s", dbText(t, d))
	}
	d.Key("gd", "esc")
	explainKey(d, "X")
	d.Settle(500 * time.Millisecond)
	if strings.Contains(planFrame(d), "runs the statement") {
		t.Fatalf("SQLite asked for an analyze card:\n%s", planFrame(d))
	}
	out, err := exec.Command("sqlite3", dbPath, "SELECT count(*) FROM orders;").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("the DELETE ran: %v %s", err, out)
	}
}

func TestExplainSanitizesAndRedactsServerText(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nSELECT 1 FROM orders;\n", 100, 30)
	s.planJSON = `[{"Plan":{"Node Type":"Foreign Scan","Total Cost":5,"Plan Rows":1,"Filter":"(dsn = 'postgres://app:s3cretpw@db.example/x')\u001b]0;evil\u0007\u001b[31m"}}]`
	explainKey(d, "x")
	dbWaitPage(t, d, "Foreign Scan")
	text := dbText(t, d)
	if strings.Contains(text, "s3cretpw") || strings.Contains(text, "\x1b") || strings.Contains(text, "\x07") {
		t.Fatalf("server text on the page is not clean:\n%q", text)
	}
	d.Key("tab") // the raw view must be clean too
	if raw := dbText(t, d); strings.Contains(raw, "s3cretpw") || strings.Contains(raw, "\x1b") {
		t.Fatalf("raw view is not clean:\n%q", raw)
	}
}

func TestExplainErrorShowsTheReasonLine(t *testing.T) {
	d, s := planDriver(t, "-- @conn pg\nSELECT * FROM nope;\n", 100, 30)
	s.failPlan = errors.New("ERROR: relation \"nope\" does not exist (SQLSTATE 42P01)\x1b[31m")
	explainKey(d, "x")
	dbWaitPage(t, d, `relation "nope" does not exist`)
	if strings.Contains(dbText(t, d), "\x1b") {
		t.Fatal("escape on the page")
	}
}

func TestExplainAnalyzeOfAWriteIsRefusedWhileATransactionIsOpen(t *testing.T) {
	d, s := planDriver(t, "-- @conn local\nSELECT 1;\n", 100, 30)
	manualMode(t, d)
	consoleRun(t, d, "INSERT INTO t VALUES (1);", true)
	d.Key("gd", "esc")
	before := len(s.events())
	explainKey(d, "X")
	if !d.WaitForText("a transaction is open", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", planFrame(d))
	}
	if strings.Contains(planFrame(d), "runs the statement") || len(s.events()) != before {
		t.Fatalf("the analyze went on: %q", s.events()[before:])
	}
}

// DB-6: the analyze card breaks lines at spaces, never inside a word.
func TestExplainAnalyzeCardWrapsAtWords(t *testing.T) {
	d, _ := planDriver(t, "-- @conn pg\nDELETE FROM orders WHERE created_at < now() - interval '2 years';\n", 80, 24)
	explainKey(d, "X")
	if !d.WaitForText("EXPLAIN ANALYZE runs the statement", 2*time.Second) {
		t.Fatalf("no card:\n%s", planFrame(d))
	}
	f := planFrame(d)
	for _, word := range []string{"transaction", "interval", "Sequences", "triggers"} {
		if !strings.Contains(f, word) {
			t.Fatalf("the card cut %q in the middle:\n%s", word, f)
		}
	}
}
