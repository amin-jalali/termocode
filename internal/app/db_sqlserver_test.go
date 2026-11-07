package app

import (
	"strings"
	"testing"
)

// DBX-62: SQL Server / Azure SQL in the add picker and the console.

func TestDBTypePickerListsSQLServer(t *testing.T) {
	for _, remote := range []bool{false, true} {
		found := false
		for _, it := range dbTypePickerItems(remote) {
			if it.ID == "sqlserver" && strings.Contains(it.Title, "SQL Server") {
				found = true
			}
		}
		if !found {
			t.Errorf("picker (remote=%v) lacks SQL Server", remote)
		}
	}
}

func TestDBAddSQLServerFillsDefaults(t *testing.T) {
	m, _ := covSvcDB(t, "")
	m.databaseStartAdd("sqlserver")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	if c := m.databases.pendingConn; c.Driver != "sqlserver" || c.Port != 1433 || c.User != "sa" || c.Database != "master" {
		t.Fatalf("pending = %+v", c)
	}
}

func TestDBConsoleSelectUsesTopOnSQLServer(t *testing.T) {
	if got := dbConsoleSelect("sqlserver", "", "[dbo].[t]", 50); got != "SELECT TOP 50 * FROM [dbo].[t];" {
		t.Fatalf("sqlserver = %q", got)
	}
	if got := dbConsoleSelect("postgres", "", `"t"`, 50); got != `SELECT * FROM "t" LIMIT 50;` {
		t.Fatalf("postgres = %q", got)
	}
}
