package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// SQL Server in the web and MCP db tools (DBX-62): the AI/MCP tools take a
// sqlserver connection (read-only through the rollback transaction), the
// MCP list shows the SQL tools for it, and the web page lists it as SQL
// with its driver name and browses the dbo schema.
func TestSQLServerInWebAndMCPDBTools(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".termocode"), 0o755)
	conns := `{"connections":[{"id":"ms","driver":"sqlserver","host":"db","port":1433,"user":"sa","database":"shop"}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := dbAIConn(root, "ms")
	if err != nil {
		t.Fatalf("AI tools refuse SQL Server: %v", err)
	}
	if !mcpServeDBSetOf(root).sql {
		t.Error("MCP lists no SQL tools for a SQL Server connection")
	}
	if !webDBSQL("sqlserver") {
		t.Error("the web cannot browse SQL Server")
	}
	if got := webDBDriver("sqlserver"); got != "sqlserver" {
		t.Errorf("web driver = %q", got)
	}
	if got := webDBDriver("neo4j"); got != "other" {
		t.Errorf("web driver of neo4j = %q", got)
	}
	if got := webDBSchema(c); got != "dbo" {
		t.Errorf("web schema = %q", got)
	}
	if got := webDBTableOf(c, "orders", false).schema; got != "dbo" {
		t.Errorf("table schema = %q", got)
	}
	if got := webDBRowsSQL(c, "orders", 50); got != "SELECT * FROM [dbo].[orders] ORDER BY (SELECT NULL) OFFSET 50 ROWS FETCH NEXT 101 ROWS ONLY" {
		t.Errorf("rows SQL = %q", got)
	}
	if _, ok := dbAIDialect("sqlserver").(dbclient.SQLServerDialect); !ok {
		t.Error("AI dialect is not T-SQL")
	}
	if !strings.Contains(webDBWhere(c), "sqlserver, shop") {
		t.Errorf("where = %q", webDBWhere(c))
	}
}

// The AI's db_explain on SQL Server (DBX-62): T-SQL has no EXPLAIN prefix,
// so the tool sends the SHOWPLAN XML plan form (never the bare statement).
func TestAIDBExplainSQLServerUsesShowplan(t *testing.T) {
	e := newAIDBEnv(t)
	conns := `{"connections":[{"id":"ms","driver":"sqlserver","host":"db","port":1433,"user":"sa","database":"shop"}]}`
	if err := os.WriteFile(filepath.Join(e.root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.gate(t, "db_explain", map[string]any{"conn": "ms", "sql": "SELECT * FROM orders"}); err != nil {
		t.Fatalf("db_explain refused SQL Server: %v", err)
	}
	if _, err := e.run(t, "db_explain", map[string]any{"conn": "ms", "sql": "SELECT * FROM orders"}); err != nil {
		t.Fatal(err)
	}
	if ran := strings.Join(e.log, ";"); !strings.Contains(ran, dbclient.SQLServerPlanForm("SELECT * FROM orders", false)) {
		t.Fatalf("ran %q", ran)
	}
}
