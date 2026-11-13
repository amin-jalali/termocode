package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// The web page browses the extra SQL engines (DBX-71) the AI tools read:
// Firebird (one schema, unqualified names), Vertica (public) and SAP HANA
// (the default schema, else the user's); Exasol stays out, as it has no
// read-only transaction.
func TestWebDBBrowsesExtraEngines(t *testing.T) {
	for _, d := range []string{"firebird", "vertica", "hana"} {
		if !webDBSQL(d) || webDBDriver(d) != d {
			t.Errorf("%s: sql %v driver %q", d, webDBSQL(d), webDBDriver(d))
		}
	}
	if webDBSQL("exasol") || webDBDriver("exasol") != "other" {
		t.Errorf("exasol: sql %v driver %q", webDBSQL("exasol"), webDBDriver("exasol"))
	}
	fb := dbclient.Conn{ID: "fb", Driver: "firebird", User: "SYSDBA", Database: "employee"}
	if s := webDBSchema(fb); s != "default" {
		t.Errorf("firebird schema = %q", s)
	}
	if got := webDBRowsSQL(fb, "EMPLOYEE", 100); got != `SELECT * FROM "EMPLOYEE" OFFSET 100 ROWS FETCH NEXT 101 ROWS ONLY` {
		t.Errorf("firebird rows = %q", got)
	}
	vt := dbclient.Conn{ID: "vt", Driver: "vertica", User: "dbadmin", Database: "vmart"}
	if got := webDBRowsSQL(vt, "orders", 0); got != `SELECT * FROM "public"."orders" LIMIT 101 OFFSET 0` {
		t.Errorf("vertica rows = %q", got)
	}
	hn := dbclient.Conn{ID: "hn", Driver: "hana", User: "system"}
	if s := webDBSchema(hn); s != "SYSTEM" {
		t.Errorf("hana schema = %q", s)
	}
	hn.Database = "SALES"
	if got := webDBRowsSQL(hn, "ORDERS", 0); got != `SELECT * FROM "SALES"."ORDERS" LIMIT 101 OFFSET 0` {
		t.Errorf("hana rows = %q", got)
	}
	if got := webDBTableOf(hn, "ORDERS", false).schema; got != "SALES" {
		t.Errorf("hana table schema = %q", got)
	}
}
