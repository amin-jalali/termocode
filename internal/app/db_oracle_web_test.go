package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Oracle in the web and MCP db tools (DBX-63): the AI/MCP tools take an
// oracle connection (SET TRANSACTION READ ONLY), MCP lists the SQL tools
// for it, the web page lists it as SQL with its driver name and browses
// the user's schema; db_explain gets the EXPLAIN PLAN form.
func TestOracleInWebAndMCPDBTools(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".termocode"), 0o755)
	conns := `{"connections":[{"id":"ora","driver":"oracle","host":"db","port":1521,"user":"app","database":"FREEPDB1"}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := dbAIConn(root, "ora")
	if err != nil {
		t.Fatalf("AI tools refuse Oracle: %v", err)
	}
	if !mcpServeDBSetOf(root).sql {
		t.Error("MCP lists no SQL tools for an Oracle connection")
	}
	if !webDBSQL("oracle") || webDBDriver("oracle") != "oracle" {
		t.Errorf("web: sql %v driver %q", webDBSQL("oracle"), webDBDriver("oracle"))
	}
	if got := webDBSchema(c); got != "APP" {
		t.Errorf("web schema = %q", got)
	}
	if got := webDBRowsSQL(c, "ORDERS", 0); got != `SELECT * FROM "APP"."ORDERS" OFFSET 0 ROWS FETCH NEXT 101 ROWS ONLY` {
		t.Errorf("rows SQL = %q", got)
	}
	if _, ok := dbAIDialect("oracle").(dbclient.OracleDialect); !ok {
		t.Error("AI dialect is not Oracle")
	}
	if got := dbAIDialect("oracle").Explain("SELECT 1 FROM dual", false); got != dbclient.OraclePlanForm("SELECT 1 FROM dual") {
		t.Errorf("AI explain = %q", got)
	}
}
