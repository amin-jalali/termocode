//go:build unix

package app

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mcpServeListNames are the names a source lists now.
func mcpServeListNames(src *mcpServeSource) string {
	var names []string
	for _, tl := range src.List() {
		names = append(names, tl.Name)
	}
	return " " + strings.Join(names, " ") + " "
}

func mcpServeWriteDBs(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, ".termocode", "databases.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Spec (MS-7): tools/list shows the database tools only where they can
// work: the SQL tools with a SQL connection, the Redis tools with a Redis
// connection. The rest of the list does not move.
func TestMCPServeListFollowsConnections(t *testing.T) {
	aiTestModel(t)
	root := t.TempDir()
	src, hub, _ := mcpServeTestSource(root, nil)
	defer hub.stop()
	if !src.ToolsCanChange() {
		t.Fatal("the source does not say its tools can change")
	}
	got := mcpServeListNames(src)
	for _, name := range []string{"db_connections", "db_schema", "db_ddl", "db_query", "redis_keys", "redis_get", "redis_command"} {
		if strings.Contains(got, " "+name+" ") {
			t.Errorf("%s listed with no connection", name)
		}
	}
	if !strings.Contains(got, " read_file ") || !strings.Contains(got, " http_send ") {
		t.Fatalf("list: %s", got)
	}
	mcpServeWriteDBs(t, root, `{"connections":[{"id":"local","driver":"postgres","host":"localhost","port":5432}]}`)
	got = mcpServeListNames(src)
	for _, name := range []string{"db_connections", "db_schema", "db_ddl", "db_query"} {
		if !strings.Contains(got, " "+name+" ") {
			t.Errorf("%s not listed with a SQL connection", name)
		}
	}
	if strings.Contains(got, " redis_") {
		t.Errorf("Redis tools listed with no Redis connection: %s", got)
	}
	mcpServeWriteDBs(t, root, mcpServeGateDBs)
	if got = mcpServeListNames(src); !strings.Contains(got, " redis_keys ") || !strings.Contains(got, " redis_command ") {
		t.Errorf("Redis tools missing: %s", got)
	}
}

// Spec (MS-7): when the set of connections changes (the Databases view
// reloads, or a connection file is saved), each initialized client gets
// notifications/tools/list_changed once; nothing when nothing changed.
func TestMCPServeListChangedOnConnections(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	root := t.TempDir()
	t.Chdir(root)
	mcpServeShortRuntime(t)
	if cmd := m.startMCPServe(); cmd == nil || m.mcpServe == nil {
		t.Fatal("not started")
	}
	defer m.stopMCPServe()
	c, err := net.Dial("unix", m.mcpServe.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	init := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"codex"}}}`)
	if !strings.Contains(mustJSON(t, init), `"listChanged":true`) {
		t.Fatalf("initialize: %v", init)
	}
	if _, err := c.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	next := func(wait time.Duration) string {
		_ = c.SetReadDeadline(time.Now().Add(wait))
		line, _ := r.ReadString('\n')
		return line
	}
	const changed = `"method":"notifications/tools/list_changed"`

	// A connection file saved in the editor.
	p := mcpServeWriteDBs(t, root, `{"connections":[{"id":"local","driver":"postgres","host":"localhost","port":5432}]}`)
	m.onMCPServeConfigSaved(p)
	if got := next(5 * time.Second); !strings.Contains(got, changed) {
		t.Fatalf("after a new connection: %q", got)
	}
	m.onMCPServeConfigSaved(p) // saved again, same set
	if got := next(200 * time.Millisecond); got != "" {
		t.Fatalf("no change, but got %q", got)
	}
	list := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if !strings.Contains(mustJSON(t, list), `"name":"db_query"`) {
		t.Fatal("db_query not listed after the notification")
	}

	// The Databases view reloads after a Redis connection was added.
	mcpServeWriteDBs(t, root, mcpServeGateDBs)
	m.ensureDatabasesLoaded()
	m.databaseRefresh()
	if got := next(5 * time.Second); !strings.Contains(got, changed) {
		t.Fatalf("after a Redis connection: %q", got)
	}
}
