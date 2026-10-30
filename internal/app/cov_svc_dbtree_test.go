package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// What a schema answer adds to the tree: a lone schema, routines and
// sequences, a table's keys, indexes and foreign keys.
func TestCovSvcDBTreeFromIntrospection(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg"}
	s.addIntrospection("pg", dbclient.IntrospectionResult{Schemas: []string{"public"}})
	if n := s.nodes["pg/schema"]; n.kind != dbNodeSchema || n.name != "public" {
		t.Fatalf("lone schema = %+v", n)
	}
	s.addIntrospection("pg/schema", dbclient.IntrospectionResult{
		Objects:   []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active", Kind: "VIEW"}},
		Routines:  []string{"touch"},
		Sequences: []string{"users_id_seq"},
	})
	got := dbRowNames(s)
	for _, want := range []string{"Tables", "users", "Views", "active", "Routines", "touch", "Sequences", "users_id_seq"} {
		if !strings.Contains(got, want) {
			t.Errorf("tree lacks %s:\n%s", want, got)
		}
	}
	s.addIntrospection("pg/schema/Tables/users", dbclient.IntrospectionResult{
		Columns:     []dbclient.Column{{Name: "id", Type: "int", PK: true}, {Name: "team_id", Type: "int"}},
		Keys:        []string{"id"},
		Indexes:     []dbclient.SchemaIndex{{Name: "users_team_idx", Columns: []string{"team_id"}}},
		ForeignKeys: []dbclient.SchemaForeignKey{{Name: "fk_team", Column: "team_id", RefTable: "teams", RefColumn: "id"}},
	})
	got = ""
	for _, c := range s.nodes["pg/schema/Tables/users"].children {
		got += s.nodes[c].name + "\n"
	}
	for _, want := range []string{"id  int", "Key · id", "Index · users_team_idx", "Foreign key · team_id → teams.id"} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
	if ids := sortedDBConnIDs(map[string]dbNode{"b": {kind: dbNodeConnection}, "a": {kind: dbNodeConnection}, "x": {kind: dbNodeTable}}); strings.Join(ids, ",") != "a,b" {
		t.Errorf("ids = %q", ids)
	}
}

// Suggestions: SQLite files (not deeper than two folders, not in .git or
// node_modules, not already connected) and connection URLs in .env.
func TestCovSvcDBSuggestionsScan(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("data/app.sqlite", "")
	write("a/b/c/deep.db", "")
	write("node_modules/x.db", "")
	write(".git/y.db", "")
	write("known.db", "")
	write("notes.txt", "")
	write(".env", "# DATABASE_URL=postgres://x@y/z\nDATABASE_URL=\"postgres://app@db:5432/app\"\nREDIS_URL=localhost\nMYSQL_URL=mysql://root@h/db\nOTHER=postgres://a@b/c\nbroken line\n")
	s := newDBState()
	s.root = root
	s.conns = []dbclient.Conn{{ID: "k", Driver: "sqlite", Path: "known.db"}}
	s.findSuggestions()
	var got []string
	for id, n := range s.nodes {
		if n.kind == dbNodeSuggestion {
			got = append(got, id)
		}
	}
	all := strings.Join(got, ",")
	for _, want := range []string{"suggest-data-app.sqlite", "suggest-env-database_url", "suggest-env-mysql_url"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in %s", want, all)
		}
	}
	for _, bad := range []string{"deep", "node_modules", ".git", "known", "notes", "redis_url", "other"} {
		if strings.Contains(strings.ToLower(all), bad) {
			t.Errorf("unexpected %s in %s", bad, all)
		}
	}
}
