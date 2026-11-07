package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbInspectState is a tree like the demo: local-pg / app / public with
// a Tables group holding users (columns loaded) and an empty Views group.
func dbInspectState(groupsLoaded bool) *dbState {
	s := &dbState{nodes: map[string]dbNode{}}
	conn := "local-pg"
	sch := conn + "/db/app/schema/public"
	s.nodes[conn] = dbNode{kind: dbNodeConnection, name: conn, path: conn, conn: conn, loaded: true, children: []string{conn + "/db/app"}}
	s.nodes[conn+"/db/app"] = dbNode{kind: dbNodeDatabase, name: "app", path: conn + "/db/app", conn: conn, loaded: true, children: []string{sch}}
	s.nodes[sch] = dbNode{kind: dbNodeSchema, name: "public", path: sch, conn: conn, loaded: true, children: []string{sch + "/Tables", sch + "/Views"}}
	s.nodes[sch+"/Tables"] = dbNode{kind: dbNodeGroup, name: "Tables", path: sch + "/Tables", conn: conn, loaded: groupsLoaded, children: []string{sch + "/Tables/users"}}
	s.nodes[sch+"/Views"] = dbNode{kind: dbNodeGroup, name: "Views", path: sch + "/Views", conn: conn, loaded: groupsLoaded}
	s.nodes[sch+"/Tables/users"] = dbNode{kind: dbNodeTable, name: "users", path: sch + "/Tables/users", conn: conn}
	// Another connection's table must not count.
	s.nodes["other/t"] = dbNode{kind: dbNodeTable, name: "ghost", path: "other/t", conn: "other"}
	s.setColumns(conn, "users", []string{"id", "email"})
	return s
}

func TestDBInspectSchemaFromTree(t *testing.T) {
	sc := dbInspectState(true).inspectSchema("local-pg")
	if !sc.Complete || !sc.Tables["public"]["users"] || sc.Tables["public"]["ghost"] {
		t.Fatalf("schema = %+v", sc)
	}
	if got := sc.Columns["users"]; len(got) != 2 {
		t.Fatalf("columns = %+v", sc.Columns)
	}
	// A table list not loaded yet: unknown names are not flagged.
	if sc := dbInspectState(false).inspectSchema("local-pg"); sc.Complete || len(sc.Tables["public"]) != 0 {
		t.Fatalf("unloaded schema = %+v", sc)
	}
}

// The diagnostics carry 0-based line and byte columns of the whole
// buffer, one per issue, errors for syntax and warnings for names.
func TestDBInspectDiagnostics(t *testing.T) {
	sc := dbInspectState(true).inspectSchema("local-pg")
	text := "-- users\nselect u.id from users u;\n\nselect u.emial\n  from usrs u, users x where x.id = (1"
	got := dbInspectDiagnostics(text, "postgres", sc)
	type want struct {
		lnum, col, endCol int
		sev               int
	}
	wants := []want{{4, 7, 11, dbDiagWarn}, {4, 36, 37, dbDiagError}}
	if len(got) != len(wants) {
		t.Fatalf("diagnostics = %+v", got)
	}
	for i, w := range wants {
		d := got[i]
		if d["lnum"] != w.lnum || d["col"] != w.col || d["end_col"] != w.endCol || d["severity"] != w.sev || d["message"] == "" {
			t.Errorf("diag %d = %+v, want %+v", i, d, w)
		}
	}
	// u.emial: u is usrs here (unknown table, no columns): not a column issue.
	if dbInspectDiagnostics("select 1", "postgres", sc) == nil {
		t.Fatal("no issues must be an empty list (clears the buffer), not nil")
	}
	_ = dbclient.IssueSyntax
}
