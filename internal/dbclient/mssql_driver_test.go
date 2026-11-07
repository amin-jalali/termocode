package dbclient

import (
	"context"
	"net"
	"strings"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
)

// DBX-62: the go-mssqldb driver is wired. These tests never reach a
// server: sql.OpenDB does not connect.

func TestSQLServerOpenDBBuildsAPool(t *testing.T) {
	db, release, err := openSQLDB(Conn{Driver: "sqlserver", Host: "db.local", Port: 1433, User: "sa", Database: "shop"}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defer db.Close()
	if _, ok := db.Driver().(*mssql.Driver); !ok {
		t.Fatalf("driver = %T", db.Driver())
	}
}

func TestSQLServerConnectorTunnelAndStartSQL(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }
	c, err := sqlServerConnector(Conn{Driver: "sqlserver", Host: "db.internal", Port: 1433, User: "sa", Dial: dial}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	mc, ok := c.(*mssql.Connector)
	if !ok {
		t.Fatalf("connector = %T", c)
	}
	hd, ok := mc.Dialer.(mssql.HostDialer)
	if !ok || hd.HostName() != "db.internal" {
		t.Fatalf("tunnel dialer = %#v (the SSH server must resolve the host)", mc.Dialer)
	}
	opts := &ConnOptions{Isolation: "serializable"}
	c, err = sqlServerConnector(Conn{Driver: "sqlserver", Host: "h", Port: 1433, Options: opts}, "")
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := c.(startSQLConnector)
	if !ok || len(sc.stmts) != 1 || sc.stmts[0] != "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE" {
		t.Fatalf("start SQL connector = %#v", c)
	}
	if inner := sc.Connector.(*mssql.Connector); !strings.Contains(inner.SessionInitSQL, "SERIALIZABLE") {
		t.Fatal("start SQL does not run again after a pool reset")
	}
}

func TestSQLServerConnectorEntraAndBadDSN(t *testing.T) {
	if _, err := sqlServerConnector(Conn{Driver: "sqlserver", Host: "x.database.windows.net", Port: 1433, User: "me", Params: map[string]string{"fedauth": "ActiveDirectoryDefault"}}, ""); err != nil {
		t.Fatalf("Entra connector: %v", err)
	}
	if _, err := sqlServerConnector(Conn{Driver: "sqlserver", Host: "x.database.windows.net", Port: 1433, Params: map[string]string{"fedauth": "NoSuchMode"}}, "secret-pw"); err == nil || strings.Contains(err.Error(), "secret-pw") {
		t.Fatalf("bad fedauth: err = %v (must fail, without the password)", err)
	}
}

func TestSQLServerPlanForms(t *testing.T) {
	forms := ExplainForms("mssql", "SELECT * FROM t;", false, false)
	if len(forms) != 1 {
		t.Fatalf("forms = %q", forms)
	}
	stmt, analyze, ok := sqlServerPlanTarget(forms[0])
	if !ok || analyze || stmt != "SELECT * FROM t" {
		t.Fatalf("plan target = %q %v %v", stmt, analyze, ok)
	}
	stmt, analyze, ok = sqlServerPlanTarget(ExplainForms("sqlserver", "UPDATE t SET a = 1", true, false)[0])
	if !ok || !analyze || stmt != "UPDATE t SET a = 1" {
		t.Fatalf("analyze target = %q %v %v", stmt, analyze, ok)
	}
	if _, _, ok := sqlServerPlanTarget("SELECT 1"); ok {
		t.Fatal("a plain statement read as a plan form")
	}
}

func TestSQLServerSavepointsAndRows(t *testing.T) {
	for in, want := range map[string]string{
		"SAVEPOINT termocode_read":             "SAVE TRANSACTION termocode_read",
		"ROLLBACK TO SAVEPOINT termocode_read": "ROLLBACK TRANSACTION termocode_read",
		"RELEASE SAVEPOINT termocode_read":     "",
	} {
		if got, ok := sqlServerSavepoint(in); !ok || got != want {
			t.Errorf("sqlServerSavepoint(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := sqlServerSavepoint("SELECT 1"); ok {
		t.Error("SELECT read as a savepoint")
	}
	if !queryReturnsRows("EXEC sp_who", "sqlserver") || queryReturnsRows("EXEC sp_who", "postgres") {
		t.Error("EXEC must return rows on SQL Server only")
	}
}

func TestSQLServerCells(t *testing.T) {
	raw := []byte{0x78, 0x56, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}
	c, ok := sqlServerCell(raw, "UNIQUEIDENTIFIER")
	if !ok || c.Text != "12345678-1234-1234-1234-56789ABCDEF0" {
		t.Fatalf("guid = %q %v", c.Text, ok)
	}
	if c, ok := sqlServerCell([]byte("12.5000"), "MONEY"); !ok || c.Text != "12.5000" || c.Kind != "number" {
		t.Fatalf("money = %+v", c)
	}
	if _, ok := sqlServerCell(int64(1), "INT"); ok {
		t.Fatal("INT is not special")
	}
}

func TestSQLServerOrderedLimitAndConfig(t *testing.T) {
	if got := OrderedLimit(SQLServerDialect{}, 10, 20); got != " ORDER BY (SELECT NULL) OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY" {
		t.Fatalf("OrderedLimit = %q", got)
	}
	if OrderedLimit(PostgresDialect{}, 10, 0) != (PostgresDialect{}).Limit(10, 0) {
		t.Fatal("OrderedLimit changed PostgreSQL")
	}
	if err := Validate(Conn{ID: "shop", Driver: "sqlserver", Host: "h", Port: 1433}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if ServerVersionQuery("mssql") != "SELECT @@VERSION" {
		t.Fatal("no version query")
	}
}
