package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// More db_* AI tool flows (ai_db_tools.go) on a scripted SQL session:
// schema reads per driver, refusals, server errors, plans, history and
// DDL. Nothing talks to a real server.

type covDBSession struct {
	intro    func(dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error)
	run      func(string) dbclient.Result
	reqs     *[]dbclient.IntrospectionRequest
	mu       *sync.Mutex
	roErr    error
	closedTo *int
}

func (s *covDBSession) Ping(context.Context) error { return nil }
func (s *covDBSession) Introspect(_ context.Context, r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	*s.reqs = append(*s.reqs, r)
	s.mu.Unlock()
	if s.intro == nil {
		return dbclient.IntrospectionResult{}, nil
	}
	return s.intro(r)
}
func (s *covDBSession) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	if s.run == nil {
		return dbclient.Result{}
	}
	return s.run(sql)
}
func (s *covDBSession) Begin(context.Context) error    { return nil }
func (s *covDBSession) Commit(context.Context) error   { return nil }
func (s *covDBSession) Rollback(context.Context) error { return nil }
func (s *covDBSession) ReadOnlySession() (dbclient.Session, error) {
	if s.roErr != nil {
		return nil, s.roErr
	}
	return s, nil
}
func (s *covDBSession) Cancel()      {}
func (s *covDBSession) Close() error { return nil }

type covDBEnv struct {
	root    string
	m       *Model
	tools   map[string]ai.Tool
	sess    *covDBSession
	reqs    []dbclient.IntrospectionRequest
	openErr error
	pws     []string
}

const covDBConns = `{"connections":[` +
	`{"id":"pg","driver":"postgres","host":"localhost","port":5432,"database":"app"},` +
	`{"id":"url","driver":"postgres","url":"postgres://app@db.example:6543/shop"},` +
	`{"id":"my","driver":"mysql","host":"localhost","port":3306,"database":"shop","name":"My Shop"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","database":"app","readOnly":true},` +
	`{"id":"cache","driver":"redis","host":"localhost","port":6379},` +
	`{"id":"bad","driver":"oracle","host":"x"},` +
	`{"id":"envpw","driver":"postgres","host":"localhost","database":"app","password":"${env:COV_DB_PW}"},` +
	`{"id":"tunnel","driver":"postgres","host":"10.0.0.5","database":"app","ssh":{"host":"bastion.example","user":"me"}}]}`

func covDBSetup(t *testing.T) *covDBEnv {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".termocode"), 0o755)
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(covDBConns), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &covDBEnv{root: root, m: &Model{}}
	e.m.databases = newDBState()
	e.m.databases.load(root)
	var mu sync.Mutex
	e.sess = &covDBSession{reqs: &e.reqs, mu: &mu}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		e.pws = append(e.pws, pw)
		if e.openErr != nil {
			return nil, e.openErr
		}
		return e.sess, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		out, _, err := fn(e.m)
		return out, err
	}
	e.tools = map[string]ai.Tool{}
	for _, tl := range aiDBTools(root, call) {
		e.tools[tl.Name] = tl
	}
	return e
}

func (e *covDBEnv) run(t *testing.T, name string, a map[string]any) (string, error) {
	t.Helper()
	return e.tools[name].Run(context.Background(), a)
}

// Connections: a problem, Redis, an unknown id and the list with names,
// targets, read-only and states.
func TestCovAIDBConnections(t *testing.T) {
	e := covDBSetup(t)
	for id, want := range map[string]string{"bad": "has a problem", "cache": "is Redis: use redis_keys", "nope": `no connection "nope"`, "": "give conn (one of: "} {
		if _, err := dbAIConn(e.root, id); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", id, err)
		}
	}
	e.m.databases.nodes["pg"] = dbNode{loading: true}
	e.m.databases.nodes["my"] = dbNode{problem: "refused"}
	out, err := e.run(t, "db_connections", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "connections", out, "pg · postgres localhost:5432/app · environment development · connecting", "url · postgres db.example:6543/shop",
		"my · name My Shop · mysql localhost:3306/shop · environment development · error: refused",
		"ro · postgres ro.example:5432/app · environment development · read-only", "bad · oracle x · environment development · problem: ")
	if got := dbAITarget(dbclient.Conn{Driver: "sqlite", Path: "/w/app.db"}); got != "sqlite /w/app.db" {
		t.Errorf("sqlite target = %q", got)
	}
	if !strings.Contains(aiDBPromptLine(e.root), "Database tools") || !strings.Contains(aiDBPromptLine(e.root), "redis") && !strings.Contains(aiDBPromptLine(e.root), "Redis") {
		t.Errorf("prompt line = %q", aiDBPromptLine(e.root))
	}
	if aiDBPromptLine(t.TempDir()) != "" {
		t.Error("prompt line without connections")
	}
	t.Chdir(e.root)
	items := e.m.dbAutoApproveItems([]string{"db:gone/write"})
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	if all := strings.Join(titles, "\n"); !strings.Contains(all, "Redis writes") || !strings.Contains(all, "db:gone/write — SQL writes on a connection that is gone") {
		t.Errorf("auto-approve rows:\n%s", all)
	}
}

// Schema reads: another PostgreSQL database, a MySQL table in the
// connection's database, nothing found, long lists, foreign keys.
func TestCovAIDBSchemaReads(t *testing.T) {
	e := covDBSetup(t)
	var many []string
	for i := 0; i < dbAIMaxObjects+3; i++ {
		many = append(many, "db"+itoa(i))
	}
	e.sess.intro = func(r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		switch {
		case r.Object != "":
			return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "int", PK: true}, {Name: "user_id", Type: "int"}},
				ForeignKeys: []dbclient.SchemaForeignKey{{Name: "fk_user", Column: "user_id", RefTable: "users", RefColumn: "id"}}}, nil
		case r.Database == "other":
			return dbclient.IntrospectionResult{Schemas: []string{"public", "audit"}}, nil
		case r.Schema == "empty":
			return dbclient.IntrospectionResult{}, nil
		}
		return dbclient.IntrospectionResult{Databases: many, Routines: []string{"f()"}, Sequences: []string{"s1"}}, nil
	}
	out, err := e.run(t, "db_schema", map[string]any{"conn": "url", "database": "other"})
	if err != nil || !strings.Contains(out, "schemas:\n  public\n  audit") || !strings.Contains(out, "pass schema (for example public)") {
		t.Errorf("other database: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "my", "table": "orders"})
	if err != nil || !strings.Contains(out, "foreign key fk_user: user_id → users.id") {
		t.Errorf("mysql table: %q %v", out, err)
	}
	if last := e.reqs[len(e.reqs)-1]; last.Schema != "shop" || last.Object != "orders" {
		t.Errorf("mysql request = %+v", last)
	}
	out, _ = e.run(t, "db_schema", map[string]any{"conn": "my"})
	covAIMustContain(t, "mysql top", out, "… and 3 more; pass schema or table", "routines:\n  f()", "sequences:\n  s1", "pass database to list its tables")
	out, _ = e.run(t, "db_schema", map[string]any{"conn": "pg", "schema": "empty"})
	if !strings.HasPrefix(out, "nothing found") {
		t.Errorf("empty schema: %q", out)
	}
	e.sess.intro = func(dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		return dbclient.IntrospectionResult{}, errors.New("permission denied for schema secret")
	}
	if _, err := e.run(t, "db_schema", map[string]any{"conn": "pg"}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("introspect error: %v", err)
	}
	e.openErr = errors.New("connection refused")
	if _, err := e.run(t, "db_schema", map[string]any{"conn": "pg"}); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("open error: %v", err)
	}
	if _, err := e.run(t, "db_schema", map[string]any{"conn": "cache"}); err == nil {
		t.Error("schema of Redis")
	}
	if got := e.tools["db_schema"].Describe(map[string]any{"conn": "pg", "schema": "public", "table": "t"}); got != "Read the schema of pg schema public table t" {
		t.Errorf("card = %q", got)
	}
	// A tunnel that is not open: ask the user to connect.
	e.openErr = nil
	if _, err := e.run(t, "db_schema", map[string]any{"conn": "tunnel"}); err == nil || !strings.Contains(err.Error(), "SSH tunnel that is not open") {
		t.Errorf("closed tunnel: %v", err)
	}
}

// db_query: a write on a read-only connection is refused even at Run; a
// server error and an open error reach the model; notices and change
// summaries are shown; a password reference resolves from the env.
func TestCovAIDBQueryEnds(t *testing.T) {
	e := covDBSetup(t)
	q := e.tools["db_query"]
	if _, err := q.Run(context.Background(), map[string]any{"conn": "ro", "sql": "DELETE FROM users"}); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("write on read-only: %v", err)
	}
	if _, err := q.Run(context.Background(), map[string]any{"conn": "pg", "sql": "BEGIN"}); err == nil {
		t.Error("BEGIN ran")
	}
	e.sess.run = func(sql string) dbclient.Result {
		if strings.HasPrefix(sql, "UPDATE") {
			return dbclient.Result{Affected: 3, Notices: []string{"NOTICE: 3 rows\nchanged"}, Elapsed: 5 * time.Millisecond}
		}
		return dbclient.Result{Err: errors.New("ERROR: relation \"nope\" does not exist\nHINT: " + strings.Repeat("x", 2100))}
	}
	out, err := q.Run(context.Background(), map[string]any{"conn": "pg", "sql": "UPDATE users SET a = 1"})
	if err != nil || !strings.Contains(out, "3 rows changed on pg (development) · 5 ms") || !strings.Contains(out, "notice: NOTICE: 3 rows changed\n") {
		t.Errorf("update: %q %v", out, err)
	}
	if _, err := q.Run(context.Background(), map[string]any{"conn": "pg", "sql": "SELECT * FROM nope"}); err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.HasSuffix(err.Error(), "…") {
		t.Errorf("server error: %v", err)
	}
	t.Setenv("COV_DB_PW", "pw-cov-123456789")
	e.sess.run = func(string) dbclient.Result {
		return dbclient.Result{Columns: []dbclient.Column{{Name: "secret"}}, Rows: [][]dbclient.Cell{{{Text: "pw-cov-123456789"}}, {{Null: true}}}, Truncated: true}
	}
	out, err = q.Run(context.Background(), map[string]any{"conn": "envpw", "sql": "SELECT secret FROM t"})
	if err != nil || strings.Contains(out, "pw-cov-123456789") || !strings.Contains(out, "[hidden]") || !strings.Contains(out, "NULL") || !strings.Contains(out, "truncated at 2 rows") {
		t.Errorf("password cell: %q %v", out, err)
	}
	if e.pws[len(e.pws)-1] != "pw-cov-123456789" {
		t.Errorf("password = %q", e.pws[len(e.pws)-1])
	}
	e.openErr = errors.New("too many connections")
	if _, err := q.Run(context.Background(), map[string]any{"conn": "pg", "sql": "SELECT 1"}); err == nil || !strings.Contains(err.Error(), "too many connections") {
		t.Errorf("open error: %v", err)
	}
	long := "SELECT " + strings.Repeat("a, ", 100) + "b FROM t"
	if got := dbAIDescribe(dbAIBuild(e.root, map[string]any{"conn": "pg", "sql": long}), "db_query"); !strings.HasSuffix(got, "…") {
		t.Errorf("long read card = %q", got)
	}
	if got := dbAIGet(e.root, nil); got.err == nil {
		t.Error("nil args")
	}
	if err := dbAIError(context.Canceled, ""); err == nil || err.Error() != dbErrorText(context.Canceled) {
		t.Errorf("cancel error = %v", err)
	}
	if (dbConsoleSettings{AIMaxKB: intPtr(-1)}).aiMaxBytes() != 32*1024 {
		t.Error("db_ai_max_kb below 1 must fall back to 32")
	}
	if got := dbAITargetDatabase(dbclient.Conn{URL: "postgres://u@h:1/a?sslmode=disable"}, "b"); got.URL != "postgres://u@h:1/b?sslmode=disable" || got.Database != "b" {
		t.Errorf("target database = %+v", got)
	}
	if _, ok := dbAIDialect("mysql").(dbclient.MySQLDialect); !ok {
		t.Error("mysql dialect")
	}
}

// db_explain: EXPLAIN in the input is refused; the plan is shown; a
// server error reaches the model; a long plan is cut.
func TestCovAIDBExplain(t *testing.T) {
	e := covDBSetup(t)
	ex := e.tools["db_explain"]
	if err := ex.Validate(map[string]any{"conn": "pg", "sql": "EXPLAIN ANALYZE SELECT 1"}); err == nil || !strings.Contains(err.Error(), "without EXPLAIN") {
		t.Errorf("explain in input: %v", err)
	}
	if _, err := ex.Run(context.Background(), map[string]any{"conn": "nope", "sql": "SELECT 1"}); err == nil {
		t.Error("unknown conn ran")
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"db_ai_max_kb": 1}`), 0o644)
	e.sess.run = func(sql string) dbclient.Result {
		var rows [][]dbclient.Cell
		for i := 0; i < 100; i++ {
			rows = append(rows, []dbclient.Cell{{Text: "Seq Scan on users  (cost=0.00..1.00 rows=1 width=4)"}, {Null: true}})
		}
		return dbclient.Result{Columns: []dbclient.Column{{Name: "QUERY PLAN"}}, Rows: rows}
	}
	out, err := ex.Run(context.Background(), map[string]any{"conn": "pg", "sql": "SELECT * FROM users"})
	if err != nil || !strings.HasPrefix(out, "plan on pg") || !strings.Contains(out, "Seq Scan") || !strings.Contains(out, "[truncated (db_ai_max_kb)]") {
		t.Errorf("plan: %.200q %v", out, err)
	}
	e.sess.run = func(string) dbclient.Result { return dbclient.Result{Err: errors.New("ERROR: syntax error")} }
	if _, err := ex.Run(context.Background(), map[string]any{"conn": "pg", "sql": "SELEC 1"}); err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("plan error: %v", err)
	}
	e.openErr = errors.New("refused")
	if _, err := ex.Run(context.Background(), map[string]any{"conn": "pg", "sql": "SELECT 1"}); err == nil {
		t.Error("open error")
	}
}

// db_ddl: refusals, a missing table, a MySQL table in the connection's
// database, indexes, foreign keys and the size cap.
func TestCovAIDBDDL(t *testing.T) {
	e := covDBSetup(t)
	ddl := e.tools["db_ddl"]
	if ddl.Validate(map[string]any{"conn": "pg", "table": " "}) == nil || ddl.Validate(map[string]any{"conn": "nope", "table": "t"}) == nil {
		t.Error("ddl validate")
	}
	if _, err := ddl.Run(context.Background(), map[string]any{"conn": "pg", "table": "nope"}); err == nil || !strings.Contains(err.Error(), `no table "nope" on pg`) {
		t.Errorf("missing table: %v", err)
	}
	if _, err := ddl.Run(context.Background(), map[string]any{"conn": "nope", "table": "t"}); err == nil {
		t.Error("unknown conn")
	}
	e.sess.intro = func(r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		return dbclient.IntrospectionResult{DDL: "CREATE TABLE orders (" + strings.Repeat("c int, ", 300) + "x int);",
			Indexes:     []dbclient.SchemaIndex{{Name: "o_x", Columns: []string{"x"}, Unique: true}, {Name: "o_c", Columns: []string{"c"}}},
			ForeignKeys: []dbclient.SchemaForeignKey{{Name: "fk", Column: "x", RefTable: "t", RefColumn: "id"}}}, nil
	}
	out, err := ddl.Run(context.Background(), map[string]any{"conn": "my", "table": "orders"})
	if err != nil || !strings.HasPrefix(out, "CREATE TABLE orders") {
		t.Fatalf("ddl: %.100q %v", out, err)
	}
	if last := e.reqs[len(e.reqs)-1]; last.Schema != "shop" {
		t.Errorf("mysql schema = %+v", last)
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"db_ai_max_kb": 1}`), 0o644)
	out, _ = ddl.Run(context.Background(), map[string]any{"conn": "pg", "table": "orders"})
	if !strings.Contains(out, "[truncated (db_ai_max_kb)]") {
		t.Errorf("not cut: %d bytes", len(out))
	}
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0o644)
	e.sess.intro = func(r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		return dbclient.IntrospectionResult{DDL: "CREATE TABLE t (x int);", Indexes: []dbclient.SchemaIndex{{Name: "t_x", Columns: []string{"x"}, Unique: true}},
			ForeignKeys: []dbclient.SchemaForeignKey{{Name: "fk", Column: "x", RefTable: "u", RefColumn: "id"}}}, nil
	}
	out, _ = ddl.Run(context.Background(), map[string]any{"conn": "pg", "table": "t"})
	if !strings.Contains(out, "-- UNIQUE INDEX t_x (x)") || !strings.Contains(out, "-- FOREIGN KEY fk (x) REFERENCES u (id)") {
		t.Errorf("ddl extras:\n%s", out)
	}
	e.sess.intro = func(dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		return dbclient.IntrospectionResult{}, errors.New("ERROR: denied")
	}
	if _, err := ddl.Run(context.Background(), map[string]any{"conn": "pg", "table": "t"}); err == nil {
		t.Error("introspect error")
	}
	e.openErr = errors.New("refused")
	if _, err := ddl.Run(context.Background(), map[string]any{"conn": "pg", "table": "t"}); err == nil {
		t.Error("open error")
	}
}

// db_history: empty, filtered by connection, long SQL cut, user runs.
func TestCovAIDBHistory(t *testing.T) {
	e := covDBSetup(t)
	if out, err := e.run(t, "db_history", map[string]any{}); err != nil || out != "no SQL history yet" {
		t.Errorf("empty: %q %v", out, err)
	}
	h := &dbclient.History{Path: dbConfigPath("history.jsonl")}
	for i, c := range []string{"pg", "my", "pg"} {
		if err := h.Append(dbclient.HistoryEntry{ID: itoa(i + 1), Conn: c, SQL: "SELECT " + strings.Repeat("x, ", 60*i) + "1", Time: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := e.run(t, "db_history", map[string]any{"conn": "pg", "limit": 1})
	if err != nil || strings.Count(out, "\n") != 1 || !strings.Contains(out, " pg ") || !strings.Contains(out, "by user") || !strings.HasSuffix(strings.TrimSpace(out), "…") {
		t.Errorf("history: %q %v", out, err)
	}
}

// databaseShowAIRun keeps the page of a user run in progress.
func TestCovAIDBShowRunKeepsUserRun(t *testing.T) {
	e := covDBSetup(t)
	m := e.m
	s := m.dbConsole()
	s.running = &dbRun{conn: "pg", sql: "SELECT pg_sleep(10)"}
	m.databaseShowAIRun(&dbRun{conn: "pg", sql: "SELECT 1", by: "ai"})
	if s.running == nil || s.running.sql != "SELECT pg_sleep(10)" {
		t.Error("the AI run took the page from the user's run")
	}
}

func intPtr(n int) *int { return &n }
