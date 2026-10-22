package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpServeDBConns: Postgres (local, production, read-only), a SQLite file
// and two Redis connections. Nothing is ever dialled: SQL sessions come
// from the fake factory (newAIDBEnv), Redis from the in-memory fake.
const mcpServeDBConns = `{"connections":[` +
	`{"id":"local","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app"},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"database":"app","environment":"production"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","port":5432,"database":"app","readOnly":true},` +
	`{"id":"lite","driver":"sqlite","path":"app.db"},` +
	`{"id":"cache","driver":"redis","host":"cache.internal","port":6379,"password":"${env:TC_REDIS_TEST_PW}"},` +
	`{"id":"prod-cache","driver":"redis","host":"prod.internal","port":6379,"environment":"production"}]}`

// mcpServeDBCard is one card the fake confirm func saw.
type mcpServeDBCardSeen struct {
	client, tool, class, human string
}

// mcpServeDBEnv is an MCP source over a workspace with database
// connections, the fake SQL factory and a fake Redis.
type mcpServeDBEnv struct {
	*aiDBEnv
	redis  *dbtest.FakeRedis
	model  Model
	src    *mcpServeSource
	hub    *mcpServeHub
	audit  *mcpServeTestAudit
	mu     sync.Mutex
	cards  []mcpServeDBCardSeen
	answer error // what the card answers (nil: Allow)
}

func newMCPServeDBEnv(t *testing.T) *mcpServeDBEnv {
	t.Helper()
	e := &mcpServeDBEnv{model: aiTestModel(t)}
	e.aiDBEnv = newAIDBEnv(t)
	if err := os.WriteFile(filepath.Join(e.root, ".termocode", "databases.json"), []byte(mcpServeDBConns), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TC_REDIS_TEST_PW", "s3cret-redis-pw")
	e.redis = dbtest.NewFakeRedis()
	e.redis.Password = "s3cret-redis-pw"
	e.redis.SetString(0, "counter", "41")
	t.Cleanup(SetRedisDialForTest(e.redis.Dial))
	t.Cleanup(e.redis.Close)
	e.src, e.hub, e.audit = mcpServeTestSource(e.root, func(_ context.Context, client string, tool ai.Tool, human string) error {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.cards = append(e.cards, mcpServeDBCardSeen{client, tool.Name, tool.Class, human})
		return e.answer
	})
	e.src.allow = &mcpServeAllowSet{}
	t.Cleanup(e.hub.stop)
	return e
}

func (e *mcpServeDBEnv) call(t *testing.T, client, tool string, args map[string]any) (mcp.CallResult, error) {
	t.Helper()
	r, err, _ := mcpServeDrive(t, &e.model, e.hub, func() (mcp.CallResult, error) {
		return e.src.Call(mcpServeCtx(client), tool, args)
	})
	return r, err
}

// tools are the exposed tools as Call builds them (the UI caller runs
// inline on the env's own model).
func (e *mcpServeDBEnv) tools() []ai.Tool {
	return mcpServeTools(e.model.aiChatTools(e.root, e.aiDBEnv.call), e.root, e.aiDBEnv.call)
}

func (e *mcpServeDBEnv) seen() []mcpServeDBCardSeen {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]mcpServeDBCardSeen(nil), e.cards...)
}

// Spec (MS-7): a read through db_query asks on the card (connection and
// SQL shown), then runs on a read-only session that is closed after; the
// base session never runs the SQL.
func TestMCPServeDBQueryReadAsksThenRunsReadOnly(t *testing.T) {
	e := newMCPServeDBEnv(t)
	r, err := e.call(t, "codex", "db_query", map[string]any{"conn": "local", "sql": "SELECT id, email FROM users"})
	if err != nil || r.IsError || !strings.Contains(mcpServeText(r), "ada@example.com") {
		t.Fatalf("read: %+v %v", r, err)
	}
	cards := e.seen()
	if len(cards) != 1 || cards[0].tool != "db_query" || cards[0].class != "db:local/read" {
		t.Fatalf("cards = %+v", cards)
	}
	for _, want := range []string{"Read on local (postgres localhost:5432/app", "read-only session", "SELECT id, email FROM users"} {
		if !strings.Contains(cards[0].human, want) {
			t.Errorf("card lacks %q:\n%s", want, cards[0].human)
		}
	}
	if len(e.opened) != 1 || len(e.opened[0].ran) != 0 || len(e.opened[0].clones) != 1 {
		t.Fatalf("sessions: %d opened", len(e.opened))
	}
	ro := e.opened[0].clones[0]
	if !ro.readOnly || len(ro.ran) != 1 || ro.ran[0] != "SELECT id, email FROM users" || ro.closed != 1 {
		t.Fatalf("read-only clone: %+v", ro)
	}
	if !strings.Contains(e.audit.all(), "by=mcp:codex db_query") || !strings.Contains(e.audit.all(), "gate=confirm:allowed") {
		t.Fatalf("audit: %s", e.audit.all())
	}
}

// Spec (MS-7): writes, DDL, more than one statement, COPY, LOAD, ATTACH,
// a writing PRAGMA, transaction statements and Redis writes are refused
// before any card (-32602) and never reach a database.
func TestMCPServeDBWritesRefused(t *testing.T) {
	e := newMCPServeDBEnv(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"db_query", map[string]any{"conn": "local", "sql": "UPDATE users SET plan = 'pro' WHERE id = 1"}},
		{"db_query", map[string]any{"conn": "local", "sql": "DELETE FROM users"}},
		{"db_query", map[string]any{"conn": "local", "sql": "DROP TABLE users"}},
		{"db_query", map[string]any{"conn": "local", "sql": "CREATE TABLE x (a int)"}},
		{"db_query", map[string]any{"conn": "local", "sql": "SELECT 1; DELETE FROM users"}},
		{"db_query", map[string]any{"conn": "local", "sql": "COPY users TO PROGRAM 'sh -c id'"}},
		{"db_query", map[string]any{"conn": "local", "sql": "LOAD 'evil.so'"}},
		{"db_query", map[string]any{"conn": "local", "sql": "WITH d AS (DELETE FROM users RETURNING *) SELECT * FROM d"}},
		{"db_query", map[string]any{"conn": "local", "sql": "SELECT nextval('s')"}},
		{"db_query", map[string]any{"conn": "local", "sql": "BEGIN"}},
		{"db_query", map[string]any{"conn": "local", "sql": "SET TRANSACTION READ WRITE"}},
		{"db_query", map[string]any{"conn": "prod", "sql": "DELETE FROM orders"}},
		{"db_query", map[string]any{"conn": "ro", "sql": "INSERT INTO t VALUES (1)"}},
		{"db_query", map[string]any{"conn": "lite", "sql": "ATTACH DATABASE '/tmp/x.db' AS x"}},
		{"db_query", map[string]any{"conn": "lite", "sql": "PRAGMA journal_mode=DELETE"}},
		{"db_query", map[string]any{"conn": "lite", "sql": "VACUUM INTO '/tmp/x.db'"}},
		{"db_query", map[string]any{"conn": "nope", "sql": "SELECT 1"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "SET counter 42"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "DEL counter"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "FLUSHALL"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "CONFIG SET requirepass x"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "CONFIG GET requirepass"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "EVAL \"return 1\" 0"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "GET a\nFLUSHALL"}},
	} {
		r, err := e.call(t, "codex", tc.tool, tc.args)
		var ce *mcp.CallError
		if !errors.As(err, &ce) || ce.Code != mcp.CodeInvalidParams || len(r.Content) != 0 {
			t.Errorf("%s %v: %+v %v", tc.tool, tc.args, r, err)
		}
	}
	if cards := e.seen(); len(cards) != 0 {
		t.Fatalf("a refused call asked: %+v", cards)
	}
	if len(e.opened) != 0 || len(e.log) != 0 {
		t.Fatalf("a refused statement reached a session: %d opened, log %v", len(e.opened), e.log)
	}
	for _, cmd := range []string{"SET", "DEL", "FLUSHALL", "CONFIG", "EVAL", "GET"} {
		if e.redis.Sent(cmd) {
			t.Errorf("%s reached Redis", cmd)
		}
	}
	// The refusal says why, and no card text could allow it.
	_, err := e.call(t, "codex", "db_query", map[string]any{"conn": "local", "sql": "DELETE FROM users"})
	if err == nil || !strings.Contains(err.Error(), "may only read") {
		t.Fatalf("refusal = %v", err)
	}
	// Run alone refuses too (no path around Validate).
	tool, _ := ai.FindTool(e.tools(), "db_query")
	if _, err := tool.Run(context.Background(), map[string]any{"conn": "local", "sql": "DELETE FROM users"}); err == nil || len(e.opened) != 0 {
		t.Fatalf("Run alone ran a write: %v", err)
	}
}

// Spec (MS-7): Deny is -32000 and nothing runs; a read-only Redis command
// and redis_get ask too, and run on the read-only session once allowed.
func TestMCPServeDBDenyRunsNothing(t *testing.T) {
	e := newMCPServeDBEnv(t)
	e.answer = &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined by the user"}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"db_query", map[string]any{"conn": "local", "sql": "SELECT * FROM users"}},
		{"redis_get", map[string]any{"conn": "cache", "key": "counter"}},
		{"redis_command", map[string]any{"conn": "cache", "command": "GET counter"}},
	} {
		r, err := e.call(t, "codex", tc.tool, tc.args)
		var ce *mcp.CallError
		if !errors.As(err, &ce) || ce.Code != mcp.CodeDeclined || len(r.Content) != 0 {
			t.Errorf("%s deny: %+v %v", tc.tool, r, err)
		}
	}
	if len(e.seen()) != 3 || len(e.opened) != 0 || e.redis.Sent("GET") || e.redis.Sent("TYPE") {
		t.Fatalf("deny ran something: cards %d, sessions %d", len(e.seen()), len(e.opened))
	}
	e.answer = nil
	r, err := e.call(t, "codex", "redis_command", map[string]any{"conn": "cache", "command": "GET counter"})
	if err != nil || r.IsError || !strings.Contains(mcpServeText(r), "41") {
		t.Fatalf("redis read: %+v %v", r, err)
	}
	if strings.Contains(mcpServeText(r), "s3cret-redis-pw") {
		t.Fatal("the Redis password reached the agent")
	}
}

// Spec (MS-7): the schema tools run at once on a read-only session.
func TestMCPServeDBSchemaAuto(t *testing.T) {
	e := newMCPServeDBEnv(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"db_connections", nil, "prod"},
		{"db_schema", map[string]any{"conn": "local", "schema": "public"}, "users"},
		{"db_ddl", map[string]any{"conn": "local", "table": "users"}, "CREATE TABLE"},
		{"redis_keys", map[string]any{"conn": "cache"}, "counter"},
	} {
		r, err := e.call(t, "codex", tc.tool, tc.args)
		if err != nil || r.IsError || !strings.Contains(mcpServeText(r), tc.want) {
			t.Errorf("%s: %+v %v", tc.tool, r, err)
		}
	}
	if cards := e.seen(); len(cards) != 0 {
		t.Fatalf("a schema read asked: %+v", cards)
	}
	for _, s := range e.opened {
		if len(s.ran) != 0 {
			t.Fatalf("a schema read ran on a writable session: %v", s.ran)
		}
	}
}

// Spec (MS-7): Always allow is per agent, tool and connection: it does not
// cover another agent, another tool or another connection, and a
// production connection never offers it.
func TestMCPServeDBAlwaysAllowPerAgentToolConn(t *testing.T) {
	e := newMCPServeDBEnv(t)
	e.src.allow.replace(map[string][]string{"codex": {"db_query:local"}})
	sel := map[string]any{"conn": "local", "sql": "SELECT 1"}
	if _, err := e.call(t, "codex", "db_query", sel); err != nil || len(e.seen()) != 0 {
		t.Fatalf("remembered call asked: %v %+v", err, e.seen())
	}
	if !strings.Contains(e.audit.all(), "gate=confirm:always") {
		t.Fatalf("audit: %s", e.audit.all())
	}
	for _, tc := range []struct {
		client, tool string
		args         map[string]any
	}{
		{"claude-code", "db_query", sel},
		{"codex", "db_query", map[string]any{"conn": "prod", "sql": "SELECT 1"}},
		{"codex", "redis_get", map[string]any{"conn": "cache", "key": "counter"}},
		{"codex", "redis_command", map[string]any{"conn": "cache", "command": "GET counter"}},
	} {
		before := len(e.seen())
		e.answer = &mcp.CallError{Code: mcp.CodeDeclined, Message: "declined"}
		e.call(t, tc.client, tc.tool, tc.args)
		if len(e.seen()) != before+1 {
			t.Errorf("%s %s %v did not ask", tc.client, tc.tool, tc.args)
		}
	}
	db, _ := ai.FindTool(e.tools(), "db_query")
	for _, tc := range []struct {
		conn, want string
	}{{"local", "db_query:local"}, {"prod", ""}} {
		a := map[string]any{"conn": tc.conn, "sql": "SELECT 1"}
		gated, _ := mcpServeGate(db, a)
		if got := mcpServeAlwaysKey(gated, a); got != tc.want {
			t.Errorf("always key on %s = %q, want %q", tc.conn, got, tc.want)
		}
	}
}
