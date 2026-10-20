package app

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// aiDBSession is a fake SQL session that records what ran on it.
type aiDBSession struct {
	mu       sync.Mutex
	name     string
	readOnly bool
	ran      []string
	closed   int
	result   func(sql string) dbclient.Result
	clones   []*aiDBSession
	log      *[]string
}

func (s *aiDBSession) Ping(context.Context) error { return nil }
func (s *aiDBSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	s.ran = append(s.ran, fmt.Sprintf("introspect %+v", req))
	s.mu.Unlock()
	if req.Object != "" {
		return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "integer", PK: true}, {Name: "email", Type: "text"}},
			Keys: []string{"id"}, Indexes: []dbclient.SchemaIndex{{Name: "users_email", Columns: []string{"email"}, Unique: true}},
			DDL: "CREATE TABLE public.users (\n  id integer,\n  email text\n);"}, nil
	}
	if req.Schema != "" {
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active", Kind: "VIEW"}}}, nil
	}
	if req.Database != "" {
		return dbclient.IntrospectionResult{Schemas: []string{"public"}}, nil
	}
	return dbclient.IntrospectionResult{Databases: []string{"app", "postgres"}}, nil
}
func (s *aiDBSession) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.ran = append(s.ran, sql)
	if s.log != nil {
		*s.log = append(*s.log, s.name+": "+sql)
	}
	s.mu.Unlock()
	if s.readOnly {
		if k, _ := dbclient.Classify(sql, "postgres"); k != dbclient.KindRead {
			return dbclient.Result{Err: errors.New("ERROR: cannot execute UPDATE in a read-only transaction (SQLSTATE 25006)")}
		}
	}
	if s.result != nil {
		return s.result(sql)
	}
	if k, _ := dbclient.Classify(sql, "postgres"); k != dbclient.KindRead {
		return dbclient.Result{Affected: 2}
	}
	return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}}, Rows: [][]dbclient.Cell{{{Text: "1"}, {Text: "ada@example.com"}}}}
}
func (s *aiDBSession) Begin(context.Context) error    { return nil }
func (s *aiDBSession) Commit(context.Context) error   { return nil }
func (s *aiDBSession) Rollback(context.Context) error { return nil }
func (s *aiDBSession) ReadOnlySession() (dbclient.Session, error) {
	c := &aiDBSession{name: s.name + "/ro", readOnly: true, result: s.result, log: s.log}
	s.mu.Lock()
	s.clones = append(s.clones, c)
	s.mu.Unlock()
	return c, nil
}
func (s *aiDBSession) Cancel() {}
func (s *aiDBSession) Close() error {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
	return nil
}

const aiDBConns = `{"connections":[` +
	`{"id":"local","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app"},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"database":"app","environment":"production"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","port":5432,"database":"app","readOnly":true}]}`

// aiDBEnv is a workspace with three Postgres connections, a fake session
// factory and a model whose UI calls run inline.
type aiDBEnv struct {
	root    string
	m       *Model
	call    aiIDECaller
	opened  []*aiDBSession
	conns   []dbclient.Conn
	pws     []string
	log     []string
	tools   []ai.Tool
	result  func(string) dbclient.Result
	factory func() error
}

func newAIDBEnv(t *testing.T) *aiDBEnv {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(aiDBConns), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &aiDBEnv{root: root}
	e.m = &Model{}
	e.m.databases = newDBState()
	e.m.databases.load(root)
	var mu sync.Mutex
	e.call = func(ctx context.Context, fn aiIDEFunc) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		out, _, err := fn(e.m)
		return out, err
	}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		if e.factory != nil {
			if err := e.factory(); err != nil {
				return nil, err
			}
		}
		s := &aiDBSession{name: "ai-" + c.ID, result: e.result, log: &e.log}
		e.opened = append(e.opened, s)
		e.conns = append(e.conns, c)
		e.pws = append(e.pws, pw)
		return s, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	e.tools = aiRedactResults(aiDBTools(root, e.call))
	return e
}

func (e *aiDBEnv) tool(t *testing.T, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(e.tools, name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// gate runs Validate and Gate the way the agent does.
func (e *aiDBEnv) gate(t *testing.T, name string, a map[string]any) (bool, string, error) {
	t.Helper()
	tl := e.tool(t, name)
	if tl.Validate != nil {
		if err := tl.Validate(a); err != nil {
			return false, "", err
		}
	}
	if tl.Gate == nil {
		return tl.Mutating, tl.Class, nil
	}
	mut, class := tl.Gate(a)
	return mut, class, nil
}

func (e *aiDBEnv) run(t *testing.T, name string, a map[string]any) (string, error) {
	t.Helper()
	return e.tool(t, name).Run(context.Background(), a)
}

// Plan §4.1: a read runs at once; a write asks with db:<conn>/write; a
// production write asks with no "Always allow"; a read-only connection
// refuses writes before the card; DDL and unknown statements ask too.
func TestAIDBQueryGate(t *testing.T) {
	e := newAIDBEnv(t)
	for _, tc := range []struct {
		conn, sql string
		ask       bool
		class     string
		err       string
	}{
		{"local", "SELECT * FROM users", false, "", ""},
		{"local", "  -- note\nSELECT 1;", false, "", ""},
		{"local", "UPDATE users SET a = 1 WHERE id = 2", true, "db:local/write", ""},
		{"local", "CREATE INDEX i ON users (email)", true, "db:local/write", ""},
		{"local", "VACUUM FULL", true, "db:local/write", ""},
		{"local", "EXPLAIN ANALYZE DELETE FROM users", true, "db:local/write", ""},
		{"prod", "SELECT count(*) FROM orders", false, "", ""},
		{"prod", "DELETE FROM orders WHERE id = 1", true, "", ""},
		{"ro", "SELECT 1", false, "", ""},
		{"ro", "INSERT INTO t VALUES (1)", false, "", "read-only"},
		{"local", "SELECT 1; DELETE FROM users", false, "", "exactly one"},
		{"local", "SELECT 1 AS $a$; DELETE FROM users", false, "", "SQL"},
		{"local", "BEGIN", false, "", "own session"},
		{"local", "", false, "", "give sql"},
		{"nope", "SELECT 1", false, "", "no connection"},
	} {
		ask, class, err := e.gate(t, "db_query", map[string]any{"conn": tc.conn, "sql": tc.sql})
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s %q: err %v, want %q", tc.conn, tc.sql, err, tc.err)
			}
			continue
		}
		if err != nil || ask != tc.ask || class != tc.class {
			t.Errorf("%s %q: ask=%v class=%q err=%v, want ask=%v class=%q", tc.conn, tc.sql, ask, class, err, tc.ask, tc.class)
		}
	}
}

// The card shows the exact SQL and the target; the password never.
func TestAIDBQueryDescribeShowsSQLAndTarget(t *testing.T) {
	e := newAIDBEnv(t)
	sql := "UPDATE users\nSET plan = 'pro'\nWHERE id = 7"
	got := e.tool(t, "db_query").Human(map[string]any{"conn": "prod", "sql": sql})
	for _, want := range []string{"Run on prod (postgres db.example:5432/app, production):", "UPDATE users\nSET plan = 'pro'\nWHERE id = 7"} {
		if !strings.Contains(got, want) {
			t.Errorf("card body lacks %q:\n%s", want, got)
		}
	}
	read := e.tool(t, "db_query").Human(map[string]any{"conn": "local", "sql": "SELECT\n  1"})
	if read != "db_query (read) on local: SELECT 1" {
		t.Errorf("read describe %q", read)
	}
}

// Reads run on a read-only clone; writes on a fresh session of their own.
// The user's session never runs AI SQL and is never closed by the AI.
func TestAIDBQueryUsesOwnSessions(t *testing.T) {
	e := newAIDBEnv(t)
	user := &aiDBSession{name: "user"}
	e.m.databases.sessions["local"] = user
	e.m.databases.passwords["local"] = "user-pass-123"
	out, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT id, email FROM users"})
	if err != nil || !strings.Contains(out, "ada@example.com") || !strings.Contains(out, "id | email") {
		t.Fatalf("read: %q %v", out, err)
	}
	if len(user.ran) != 0 || len(user.clones) != 1 || len(user.clones[0].ran) != 1 || user.clones[0].closed != 1 {
		t.Fatalf("read did not use a closed read-only clone: user ran %v, clones %d", user.ran, len(user.clones))
	}
	out, err = e.run(t, "db_query", map[string]any{"conn": "local", "sql": "UPDATE users SET a = 1 WHERE id = 2"})
	if err != nil || !strings.Contains(out, "2 rows changed on local") {
		t.Fatalf("write: %q %v", out, err)
	}
	if len(user.ran) != 0 || user.closed != 0 {
		t.Fatalf("the AI used or closed the user's session: %v closed=%d", user.ran, user.closed)
	}
	if len(e.opened) != 1 || e.opened[0].readOnly || e.opened[0].closed != 1 || e.pws[0] != "user-pass-123" {
		t.Fatalf("write session: opened %d", len(e.opened))
	}
	// Both runs are on the page model and in the history, by AI.
	s := e.m.databases.console
	if s == nil || s.shown == nil || s.shown.by != "ai" || !strings.Contains(s.shown.sql, "UPDATE") {
		t.Fatalf("result page not updated by AI: %+v", s)
	}
}

// Without a user session the read path opens a session, clones it read-only
// and closes both. The password comes from the secret store.
func TestAIDBQueryReadWithoutUserSessionUsesSecretStore(t *testing.T) {
	e := newAIDBEnv(t)
	store := databaseSecretStore()
	if store == nil {
		t.Skip("no secret store")
	}
	if err := store.Set("db.local", "store-pass-456"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if len(e.opened) != 1 || e.pws[0] != "store-pass-456" || e.opened[0].closed != 1 || len(e.opened[0].ran) != 0 {
		t.Fatalf("base session: %d opened", len(e.opened))
	}
	if len(e.opened[0].clones) != 1 || !e.opened[0].clones[0].readOnly || len(e.opened[0].clones[0].ran) != 1 || e.opened[0].clones[0].closed != 1 {
		t.Fatal("the read did not run on a closed read-only clone")
	}
}

// The classifier is only a hint: a "read" the server refuses comes back as
// the server's error, and nothing is written.
func TestAIDBQueryServerRefusalReachesModel(t *testing.T) {
	e := newAIDBEnv(t)
	e.result = func(sql string) dbclient.Result {
		return dbclient.Result{Err: errors.New("ERROR: permission denied for table secrets\nHINT: ask the owner\x1b[31m")}
	}
	_, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT * FROM secrets"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "HINT: ask the owner") || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("server error: %v", err)
	}
}

// Passwords never reach the model: not in rows, not in errors.
func TestAIDBQueryRedaction(t *testing.T) {
	e := newAIDBEnv(t)
	const pw = "pw-very-secret-789"
	e.m.databases.passwords["local"] = pw
	e.result = func(sql string) dbclient.Result {
		if strings.Contains(sql, "fail") {
			return dbclient.Result{Err: fmt.Errorf("failed to connect to postgres://app:%s@localhost/app: password authentication failed", pw)}
		}
		return dbclient.Result{Columns: []dbclient.Column{{Name: "k"}, {Name: "v"}}, Rows: [][]dbclient.Cell{{{Text: "password"}, {Text: pw}}, {{Text: "dsn"}, {Text: "postgres://app:" + pw + "@x/app"}}}}
	}
	out, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT k, v FROM settings"})
	if err != nil || strings.Contains(out, pw) || !strings.Contains(out, "[hidden]") {
		t.Fatalf("rows leak the password: %q %v", out, err)
	}
	_, err = e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT fail"})
	if err == nil || strings.Contains(err.Error(), pw) {
		t.Fatalf("error leaks the password: %v", err)
	}
	for _, line := range e.log {
		if strings.Contains(line, pw) {
			t.Fatalf("SQL log leaks the password: %s", line)
		}
	}
	// The history on disk holds no password either.
	if b, _ := os.ReadFile(dbConfigPath("history.jsonl")); strings.Contains(string(b), pw) {
		t.Fatalf("history leaks the password: %s", b)
	}
}

// The answer is capped at db_ai_max_kb and says so; max_rows caps the rows.
func TestAIDBQueryOutputCap(t *testing.T) {
	e := newAIDBEnv(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode"), 0o700); err != nil {
		t.Fatal(err)
	}
	p, _ := settingsConfigPath()
	if err := os.WriteFile(p, []byte(`{"db_ai_max_kb": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.result = func(sql string) dbclient.Result {
		r := dbclient.Result{Columns: []dbclient.Column{{Name: "n"}, {Name: "text"}}}
		for i := 0; i < 500; i++ {
			r.Rows = append(r.Rows, []dbclient.Cell{{Text: fmt.Sprint(i)}, {Text: strings.Repeat("x", 200)}})
		}
		return r
	}
	out, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT * FROM big", "max_rows": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 1200 || !strings.Contains(out, "[truncated;") || !strings.Contains(out, "use max_rows or a WHERE") {
		t.Fatalf("answer not capped (%d bytes):\n%s", len(out), out)
	}
	if strings.Contains(out, strings.Repeat("x", 81)) {
		t.Fatal("a cell longer than 80 characters reached the model")
	}
}

// db_schema, db_ddl and db_explain run on read-only sessions only.
func TestAIDBSchemaDDLExplain(t *testing.T) {
	e := newAIDBEnv(t)
	user := &aiDBSession{name: "user"}
	e.m.databases.sessions["local"] = user
	e.m.databases.passwords["local"] = ""
	out, err := e.run(t, "db_schema", map[string]any{"conn": "local"})
	if err != nil || !strings.Contains(out, "databases:\n  app\n  postgres") || !strings.Contains(out, "schemas:\n  public") {
		t.Fatalf("schema: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "local", "schema": "public"})
	if err != nil || !strings.Contains(out, "users (base table)") || !strings.Contains(out, "active (view)") {
		t.Fatalf("objects: %q %v", out, err)
	}
	out, err = e.run(t, "db_schema", map[string]any{"conn": "local", "table": "users"})
	if err != nil || !strings.Contains(out, "id integer [PK]") || !strings.Contains(out, "index users_email: unique (email)") {
		t.Fatalf("table: %q %v", out, err)
	}
	out, err = e.run(t, "db_ddl", map[string]any{"conn": "local", "table": "users"})
	if err != nil || !strings.Contains(out, "CREATE TABLE public.users") || !strings.Contains(out, "-- UNIQUE INDEX users_email (email)") {
		t.Fatalf("ddl: %q %v", out, err)
	}
	if _, err := e.run(t, "db_explain", map[string]any{"conn": "local", "sql": "DELETE FROM users WHERE id = 1"}); err != nil {
		t.Fatal(err)
	}
	if len(user.ran) != 0 {
		t.Fatalf("the user's session ran AI SQL: %v", user.ran)
	}
	last := user.clones[len(user.clones)-1]
	if !last.readOnly || len(last.ran) != 1 || last.ran[0] != "EXPLAIN (FORMAT TEXT) DELETE FROM users WHERE id = 1" {
		t.Fatalf("explain ran %v", last.ran)
	}
	for _, c := range user.clones {
		if c.closed != 1 {
			t.Fatal("a read-only clone stayed open")
		}
	}
	if err := e.tool(t, "db_explain").Validate(map[string]any{"conn": "local", "sql": "EXPLAIN ANALYZE SELECT 1"}); err == nil {
		t.Fatal("db_explain accepted EXPLAIN ANALYZE")
	}
	// Another PostgreSQL database: a fresh session on it, never the user's.
	if _, err := e.run(t, "db_schema", map[string]any{"conn": "local", "database": "other"}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.conns); n == 0 || e.conns[n-1].Database != "other" {
		t.Fatalf("schema of another database opened %v", e.conns)
	}
}

// db_connections and db_history list what the plan names; no password.
func TestAIDBConnectionsAndHistory(t *testing.T) {
	e := newAIDBEnv(t)
	e.m.databases.sessions["local"] = &aiDBSession{name: "user"}
	out, err := e.run(t, "db_connections", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"local · postgres localhost:5432/app · environment development · connected", "prod · postgres db.example:5432/app · environment production · not connected", "ro · postgres ro.example:5432/app · environment development · read-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("connections lack %q:\n%s", want, out)
		}
	}
	if _, err := e.run(t, "db_query", map[string]any{"conn": "local", "sql": "SELECT 42"}); err != nil {
		t.Fatal(err)
	}
	// The history append is a command the UI runs; run it here.
	h := &dbclient.History{Path: dbConfigPath("history.jsonl")}
	if err := h.Append(dbclient.HistoryEntry{ID: "1", Conn: "prod", SQL: "SELECT 1", Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(dbclient.HistoryEntry{ID: "2", Conn: "local", SQL: "SELECT 42", Kind: "ai", Rows: 1}); err != nil {
		t.Fatal(err)
	}
	out, err = e.run(t, "db_history", map[string]any{"conn": "local"})
	if err != nil || !strings.Contains(out, "#2 ") || !strings.Contains(out, "by ai: SELECT 42") || strings.Contains(out, "prod") {
		t.Fatalf("history: %q %v", out, err)
	}
}

// databaseShowAIRun appends a history entry with kind "ai".
func TestAIDBRunGoesToHistoryByAI(t *testing.T) {
	newAIDBEnv(t)
	m := &Model{}
	cmd := m.databaseShowAIRun(&dbRun{conn: "local", sql: "SELECT 7", by: "ai", result: dbclient.Result{Columns: []dbclient.Column{{Name: "a"}}}})
	runCmds(cmd)
	b, err := os.ReadFile(dbConfigPath("history.jsonl"))
	if err != nil || !strings.Contains(string(b), `"kind":"ai"`) || !strings.Contains(string(b), "SELECT 7") {
		t.Fatalf("history: %s %v", b, err)
	}
}

func runCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmds(c)
		}
	}
}

// The auto-approve picker lists db:<conn>/write for every SQL connection
// except production ones.
func TestAIDBAutoApprovePickerRows(t *testing.T) {
	e := newAIDBEnv(t)
	t.Chdir(e.root)
	items := e.m.dbAutoApproveItems([]string{"db:local/write", "db:gone/write"})
	var got []string
	for _, it := range items {
		got = append(got, it.ID+"="+it.Hint)
	}
	joined := strings.Join(got, ",")
	if joined != "db:local/write=AUTO,db:ro/write=ask,db:gone/write=AUTO" {
		t.Fatalf("rows %s", joined)
	}
}

// The system prompt names the db tools only when connections exist.
func TestAIDBPromptLine(t *testing.T) {
	e := newAIDBEnv(t)
	if l := aiDBPromptLine(e.root); !strings.Contains(l, "db_explain") || !strings.Contains(l, "production") {
		t.Fatalf("prompt line %q", l)
	}
	if l := aiDBPromptLine(t.TempDir()); l != "" {
		t.Fatalf("prompt line without connections: %q", l)
	}
}

// Reviewer note (DB-0): every caller passes the connection's dialect to
// dbclient.Split / dbclient.Classify; an empty dialect lets "$tag$" hide a
// second statement. This scans every non-test Go file of the module.
func TestDBSplitClassifyCallersPassDialect(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "testdata" || strings.HasPrefix(n, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		inDBClient := f.Name.Name == "dbclient"
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "dbclient" {
					name = fn.Sel.Name
				}
			case *ast.Ident:
				if inDBClient {
					name = fn.Name
				}
			}
			if name != "Split" && name != "Classify" {
				return true
			}
			checked++
			pos := fset.Position(call.Pos())
			if len(call.Args) != 2 {
				t.Errorf("%s: %s without a dialect", pos, name)
				return true
			}
			if lit, ok := call.Args[1].(*ast.BasicLit); ok {
				t.Errorf("%s: %s with a literal dialect %s; pass the connection's driver", pos, name, lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 5 {
		t.Fatalf("found only %d calls; the scan is broken", checked)
	}
}
