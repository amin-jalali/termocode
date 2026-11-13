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
	if err := DriverMissing("oracle"); err != nil {
		t.Fatalf("oracle reported missing: %v", err) // go-ora is built in (DBX-63)
	}
	if DriverMissing("postgres") != nil {
		t.Fatal("postgres reported missing")
	}
}

// From a URL (DBX-62): sqlserver:// (and mssql://, azuresql://) reads the
// host, port, user, ?database= and the other options; the password moves
// out of the saved URL; an instance name stays in the URL path.
func TestParseURLSQLServer(t *testing.T) {
	c, err := ParseURL("sqlserver://sa:s3cret@db.local:1444?database=shop&TrustServerCertificate=true")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "sqlserver" || c.Host != "db.local" || c.Port != 1444 || c.User != "sa" || c.Database != "shop" {
		t.Fatalf("conn = %+v", c)
	}
	if c.Password != "s3cret" || strings.Contains(c.URL, "s3cret") {
		t.Fatalf("password kept in the URL: %+v", c)
	}
	if c.Params["TrustServerCertificate"] != "true" || c.Params["database"] != "" {
		t.Fatalf("params = %v", c.Params)
	}
	if dsn := SQLServerDSN(c, c.Password); strings.Count(dsn, "database=") != 1 || !strings.Contains(dsn, "sa:s3cret@db.local:1444") {
		t.Fatalf("dsn = %s", dsn)
	}
	for _, raw := range []string{"mssql://sa@db", "azuresql://me@x.database.windows.net?database=d"} {
		c, err := ParseURL(raw)
		if err != nil || c.Driver != "sqlserver" || c.Port != 1433 {
			t.Errorf("%s = %+v, %v", raw, c, err)
		}
	}
	c, err = ParseURL("sqlserver://sa@db/SQLEXPRESS?database=app")
	if err != nil || c.Database != "app" || !strings.Contains(SQLServerDSN(c, ""), "/SQLEXPRESS") {
		t.Fatalf("instance: %+v, %v", c, err)
	}
	if c.ID = "app"; Validate(c) != nil {
		t.Fatalf("validate: %v", Validate(c))
	}
}
