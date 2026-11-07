package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-28: the tree lists only the databases, schemas and objects the
// connection's options let through.
func TestDBConnOptionsFilterTheTree(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres", Options: &dbclient.ConnOptions{Databases: []string{"app*"}, Schemas: []string{"public"}, Objects: []string{"!tmp_*"}}}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg"}
	s.addIntrospection("pg", dbclient.IntrospectionResult{Databases: []string{"app", "app_test", "postgres"}})
	if _, ok := s.nodes["pg/db/postgres"]; ok {
		t.Fatal("database postgres is not in the databases list but shows")
	}
	if _, ok := s.nodes["pg/db/app_test"]; !ok {
		t.Fatal("app_test matches app* but is hidden")
	}
	s.addIntrospection("pg/db/app", dbclient.IntrospectionResult{Schemas: []string{"public", "audit"}})
	if _, ok := s.nodes["pg/db/app/schema/audit"]; ok {
		t.Fatal("schema audit shows")
	}
	sp := "pg/db/app/schema/public"
	s.addIntrospection(sp, dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "tmp_load", Kind: "BASE TABLE"}}, Routines: []string{"tmp_fn", "f"}})
	if _, ok := s.nodes[sp+"/Tables/users"]; !ok {
		t.Fatal("users is hidden")
	}
	if _, ok := s.nodes[sp+"/Tables/tmp_load"]; ok {
		t.Fatal("!tmp_* did not hide tmp_load")
	}
	if _, ok := s.nodes[sp+"/Routines/tmp_fn"]; ok {
		t.Fatal("!tmp_* did not hide routine tmp_fn")
	}
}

// DBX-28: a table in readOnlyTables cannot be edited from the grid.
func TestDBConnOptionsReadOnlyTable(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 120, h: 40, databases: newDBState()}
	m.databases.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres", Options: &dbclient.ConnOptions{ReadOnlyTables: []string{"public.audit_*"}}}}
	cols := []dbclient.Column{{Name: "id"}}
	mk := func(name string) *dbTab {
		tv := &dbTableView{dbTable: dbTable{conn: "pg", driver: "postgres", schema: "public", name: name}, keys: []string{"id"}}
		r := &dbRun{conn: "pg", by: "user", tv: tv, result: dbclient.Result{Columns: cols, Rows: [][]dbclient.Cell{{{Text: "1"}}}}}
		return &dbTab{run: r}
	}
	tg, _ := m.databaseEditTarget(mk("audit_log"))
	if tg == nil || tg.why == "" {
		t.Fatalf("audit_log is editable: %+v", tg)
	}
	tg, _ = m.databaseEditTarget(mk("users"))
	if tg == nil || tg.why != "" {
		t.Fatalf("users is read-only: %+v", tg)
	}
}

// DBX-28: txMode "manual" saved with the connection starts it in manual
// mode once; the user's own switch is kept afterwards.
func TestDBConnOptionsTxModeManual(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres", Options: &dbclient.ConnOptions{TxMode: "manual"}}, {ID: "my", Driver: "mysql"}}
	s.applyConnTxMode("pg")
	s.applyConnTxMode("my")
	if !s.manualMode("pg") || s.manualMode("my") {
		t.Fatalf("manual = %v", s.manual)
	}
	delete(s.manual, "pg") // the user picks auto-commit
	s.applyConnTxMode("pg")
	if s.manualMode("pg") {
		t.Fatal("a reconnect overrode the user's choice")
	}
}
