package dbclient

import (
	"net/url"
	"strings"
	"testing"
)

func TestSQLServerDriverAliases(t *testing.T) {
	for _, d := range []string{"mssql", "sqlserver", "azuresql"} {
		if normDriver(d) != "sqlserver" {
			t.Errorf("normDriver(%q) = %q", d, normDriver(d))
		}
	}
	if defaultPort("sqlserver") != 1433 {
		t.Fatal("no default port 1433")
	}
}

func TestSQLServerDSN(t *testing.T) {
	got := SQLServerDSN(Conn{Driver: "sqlserver", Host: "db.local", Port: 1433, User: "sa", Database: "shop"}, "p@ss")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "sqlserver" || u.Host != "db.local:1433" || u.User.Username() != "sa" || u.Query().Get("database") != "shop" {
		t.Fatalf("dsn = %s", got)
	}
	if p, _ := u.User.Password(); p != "p@ss" {
		t.Fatalf("password = %q", p)
	}
	az := SQLServerDSN(Conn{Driver: "sqlserver", Host: "x.database.windows.net", Port: 1433, User: "me", Database: "d", Params: map[string]string{"fedauth": "ActiveDirectoryDefault"}}, "")
	if !strings.Contains(az, "encrypt=true") || !strings.Contains(az, "fedauth=ActiveDirectoryDefault") {
		t.Fatalf("azure dsn = %s", az)
	}
	if !IsAzureSQLHost("x.database.windows.net") || IsAzureSQLHost("windows.net.evil.com") {
		t.Fatal("IsAzureSQLHost is wrong")
	}
	if got := SQLServerDSN(Conn{Driver: "sqlserver", URL: "sqlserver://u@h:1433?database=d"}, "pw"); !strings.Contains(got, "u:pw@h:1433") {
		t.Fatalf("url dsn = %s", got)
	}
}

func TestSQLServerDialectWired(t *testing.T) {
	if _, ok := DialectFor("mssql").(SQLServerDialect); !ok {
		t.Fatal("no SQL Server dialect")
	}
	if err := DriverMissing("sqlserver"); err != nil {
		t.Fatalf("SQL Server driver reported missing: %v", err) // go-mssqldb is built in (DBX-62)
	}
	if DriverMissing("oracle") == nil {
		t.Fatal("oracle not reported missing")
	}
	if DriverMissing("postgres") != nil {
		t.Fatal("postgres reported missing")
	}
}
