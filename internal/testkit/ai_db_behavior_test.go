package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// DB-5: the AI's db_* tools through the real app, a fake model and the
// user's sqlite3 CLI (skipped when it is missing). Reads run in a
// sqlite3 -readonly sidecar; writes ask with the exact SQL.

func aiDBDriver(t *testing.T, srvURL, conns string, auto ...string) (*Driver, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	var dbPath string
	files := textFiles()
	files[".termocode/databases.json"] = conns
	approve, _ := json.Marshal(auto)
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: files,
		Config: map[string]string{"config.json": `{"ai_auto_approve":` + string(approve) + `}`},
		Setup: func(tb testing.TB, dir string) {
			tb.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			tb.Setenv("TERMOCODE_AI_MODEL", "fake")
			tb.Setenv("TERMOCODE_AI_BASE_URL", srvURL+"/v1")
			dbPath = filepath.Join(dir, "app.db")
			sql := "CREATE TABLE users(id INTEGER PRIMARY KEY, email TEXT NOT NULL); CREATE UNIQUE INDEX users_email ON users(email);" +
				"INSERT INTO users(email) VALUES ('ada@example.com'),('bob@example.com'),('cy@example.com');"
			if out, err := exec.Command("sqlite3", dbPath, sql).CombinedOutput(); err != nil {
				tb.Fatalf("fixture: %v %s", err, out)
			}
		}})
	return d, dbPath
}

func sqliteOne(t *testing.T, path, sql string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", path, sql).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3 %q: %v %s", sql, err, out)
	}
	return strings.TrimSpace(string(out))
}

func aiDBPage(d *Driver) string {
	v, _ := d.Model().NvimLua(`local b=vim.g.termocode_db_buf; if not b or not vim.api.nvim_buf_is_valid(b) then return '' end; return table.concat(vim.api.nvim_buf_get_lines(b,0,-1,false),'\n')`)
	return v
}

const aiDBLite = `{"connections":[{"id":"lite","driver":"sqlite","path":"app.db"}]}`

// Spec: reads (schema, DDL, SELECT, plan, history) run without a card; a
// SELECT that calls writefile asks (Deny keeps the disk clean); a DELETE asks
// with the SQL and the connection, offers "Always allow db:lite/write",
// and runs after Allow. The page says "by AI"; history and audit record it.
func TestAIDBSQLiteReadsAndWrite(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"db_connections", map[string]any{}},
		toolCall{"db_schema", map[string]any{"conn": "lite"}},
		toolCall{"db_ddl", map[string]any{"conn": "lite", "table": "users"}},
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "SELECT id, email FROM users ORDER BY id"}},
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "SELECT writefile('pwned.txt', 'x')"}},
		toolCall{"db_explain", map[string]any{"conn": "lite", "sql": "SELECT * FROM users WHERE email = 'x'"}},
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "DELETE FROM users WHERE id = 2"}},
		toolCall{"db_history", map[string]any{"conn": "lite"}})
	d, dbPath := aiDBDriver(t, srv.URL, aiDBLite)
	askAI(d, "clean up bob")
	// A SELECT that calls a file-writing function is not a plain read: it asks.
	waitConfirm(d)
	if !strings.Contains(screen(d), "SELECT writefile('pwned.txt', 'x')") {
		t.Fatalf("writefile card:\n%s", screen(d))
	}
	clickConfirm(d, "Deny")
	if !d.WaitFor(func() bool {
		return strings.Contains(screen(d), "DELETE FROM users WHERE id = 2") && strings.Contains(screen(d), "AI wants to")
	}, 8*time.Second) {
		t.Fatalf("no DELETE card:\n%s", screen(d))
	}
	card := screen(d)
	for _, want := range []string{"db_query", "Run on lite", "DELETE FROM users WHERE id = 2", "Always allow db:lite/write"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	if n := sqliteOne(t, dbPath, "SELECT count(*) FROM users"); n != "3" {
		t.Fatalf("the write ran before Allow: %s rows", n)
	}
	clickAIAllow(d)
	httpWaitDone(d)
	r := s.toolResults()
	if len(r) != 8 {
		t.Fatalf("results %q", r)
	}
	checks := []struct {
		i    int
		want []string
	}{
		{0, []string{"lite · sqlite app.db"}},
		{1, []string{"users (table)"}},
		{2, []string{"CREATE TABLE users", "-- UNIQUE INDEX users_email (email)"}},
		{3, []string{"3 rows on lite", "id | email", "2 | bob@example.com"}},
		{4, []string{"declined"}},
		{5, []string{"plan on lite"}},
		{6, []string{"1 row changed on lite"}},
		{7, []string{"by ai: DELETE FROM users WHERE id = 2", "by ai: SELECT id, email FROM users ORDER BY id"}},
	}
	for _, c := range checks {
		for _, w := range c.want {
			if !strings.Contains(r[c.i], w) {
				t.Errorf("result %d lacks %q:\n%s", c.i, w, r[c.i])
			}
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), "pwned.txt")); err == nil {
		t.Errorf("a SELECT wrote a file through the read-only session")
	}
	if n := sqliteOne(t, dbPath, "SELECT count(*) FROM users"); n != "2" {
		t.Errorf("allowed DELETE did not run: %s rows", n)
	}
	if !d.WaitFor(func() bool { return strings.Contains(aiDBPage(d), "by AI") }, 3*time.Second) {
		t.Errorf("result page not marked by AI:\n%s", aiDBPage(d))
	}
	audit := strings.Join(d.Model().AIAuditLines(), "\n")
	if !strings.Contains(audit, "db_query") || !strings.Contains(audit, "user allowed") {
		t.Errorf("audit:\n%s", audit)
	}
	hist, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db", "history.jsonl"))
	if !strings.Contains(string(hist), `"kind":"ai"`) {
		t.Errorf("history: %s", hist)
	}
}

// Spec: a production connection always asks before a write, with no
// "Always allow" — even when db:<conn>/write was saved by hand — and Deny
// leaves the data alone.
func TestAIDBProductionWriteAlwaysAsks(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "SELECT count(*) FROM users"}},
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "UPDATE users SET email = 'x@example.com' WHERE id = 1"}})
	d, dbPath := aiDBDriver(t, srv.URL, `{"connections":[{"id":"lite","driver":"sqlite","path":"app.db","environment":"production"}]}`, "db:lite/write")
	askAI(d, "fix ada")
	waitConfirm(d)
	card := screen(d)
	if strings.Contains(card, "Always allow") || !strings.Contains(card, "production") || !strings.Contains(card, "UPDATE users SET email") {
		t.Fatalf("production card:\n%s", card)
	}
	clickConfirm(d, "Deny")
	httpWaitDone(d)
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[0], "1 row on lite (production)") || !strings.Contains(r[1], "declined") {
		t.Fatalf("results %q", r)
	}
	if v := sqliteOne(t, dbPath, "SELECT email FROM users WHERE id = 1"); v != "ada@example.com" {
		t.Fatalf("denied UPDATE changed the row: %s", v)
	}
}

// Spec: a read-only connection refuses an AI write without a card.
func TestAIDBReadOnlyConnectionRefusesWrite(t *testing.T) {
	s, srv := fakeToolModel(t, toolCall{"db_query", map[string]any{"conn": "lite", "sql": "DELETE FROM users"}})
	d, dbPath := aiDBDriver(t, srv.URL, `{"connections":[{"id":"lite","driver":"sqlite","path":"app.db","readOnly":true}]}`)
	askAI(d, "delete all")
	httpWaitDone(d)
	if strings.Contains(screen(d), "AI wants to") {
		t.Fatalf("a read-only connection asked:\n%s", screen(d))
	}
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "read-only") {
		t.Fatalf("results %q", r)
	}
	if n := sqliteOne(t, dbPath, "SELECT count(*) FROM users"); n != "3" {
		t.Fatalf("rows deleted: %s", n)
	}
}
