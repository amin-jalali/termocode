package app

import "testing"

// Rerunning one pgTAP test runs runtests() on its schema with an exact
// name pattern.
func TestDBPgTAPCaseSQL(t *testing.T) {
	if got := dbPgTAPCaseSQL("public.test_users"); got != `SELECT * FROM runtests('public'::name, '^test_users$'::text);` {
		t.Fatalf("sql %q", got)
	}
	if got := dbPgTAPCaseSQL("test.x"); got != `SELECT * FROM runtests('test'::name, '^x$'::text);` {
		t.Fatalf("sql %q", got)
	}
	if got := dbPgTAPCaseSQL("test_a+b"); got != `SELECT * FROM runtests('^test_a\+b$'::text);` {
		t.Fatalf("sql %q", got)
	}
}

func TestDBPgTAPPaletteIDHasAPolicy(t *testing.T) {
	if _, ok := aiActionPolicies["db-pgtap-run"]; !ok {
		t.Fatal("db-pgtap-run has no AI policy")
	}
}
