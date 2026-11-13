package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// DBX-65: the AI's db tools read ClickHouse, DuckDB, Trino/Presto and
// remote libSQL too, each through its own read-only session (the
// server's readonly setting, duckdb -readonly, a READ ONLY transaction,
// PRAGMA query_only), with the engine's own dialect.
func TestAIDBToolsAcceptAnalyticsEngines(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.URL.Query().Get("readonly")+" "+string(body))
		mu.Unlock()
		_, _ = io.WriteString(w, "[\"id\",\"name\"]\n[\"UInt8\",\"String\"]\n[1,\"ada\"]\n")
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	conns := `{"connections":[` +
		`{"id":"ch","driver":"clickhouse","host":"` + host + `","port":` + port + `,"user":"default"},` +
		`{"id":"duck","driver":"duckdb","path":"data.duckdb"},` +
		`{"id":"tr","driver":"trino","host":"localhost","port":8080,"user":"ada","database":"hive"},` +
		`{"id":"pr","driver":"presto","host":"localhost","port":8080,"user":"ada","database":"hive"},` +
		`{"id":"turso","driver":"libsql","url":"libsql://db.example.turso.io"}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ch", "duck", "tr", "pr", "turso"} {
		if _, err := dbAIConn(root, id); err != nil {
			t.Errorf("dbAIConn(%s) = %v, want the connection", id, err)
		}
	}
	for driver, want := range map[string]string{"clickhouse": "dbclient.ClickHouseDialect", "duckdb": "dbclient.DuckDBDialect", "trino": "dbclient.TrinoDialect", "libsql": "dbclient.SQLiteDialect"} {
		if got := typeName(dbAIDialect(driver)); got != want {
			t.Errorf("dbAIDialect(%s) = %s, want %s", driver, got, want)
		}
	}

	m := &Model{databases: newDBState()}
	m.databases.load(root)
	var cmu sync.Mutex
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		cmu.Lock()
		defer cmu.Unlock()
		out, _, err := fn(m)
		return out, err
	}
	e := &aiDBEnv{root: root, m: m, call: call, tools: aiRedactResults(aiDBTools(root, call))}
	out, err := e.run(t, "db_query", map[string]any{"conn": "ch", "sql": "SELECT id, name FROM users"})
	if err != nil {
		t.Fatalf("db_query on ClickHouse: %v", err)
	}
	if !strings.Contains(out, "ada") {
		t.Fatalf("db_query on ClickHouse lost the row:\n%s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	var read bool
	for _, s := range seen {
		if strings.Contains(s, "FROM users") {
			read = true
			if !strings.HasPrefix(s, "2 ") {
				t.Errorf("the AI's read went without readonly=2: %q", s)
			}
		}
	}
	if !read {
		t.Fatalf("the read never reached the server: %q", seen)
	}
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }
