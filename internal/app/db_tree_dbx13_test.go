package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTreeRolesGroup(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{"c": {kind: dbNodeConnection, name: "c", path: "c", conn: "c"}}}
	s.addIntrospection("c", dbclient.IntrospectionResult{Databases: []string{"app"}, Roles: []string{"alice", "bob"}})
	g, ok := s.nodes["c/Roles"]
	if !ok || g.kind != dbNodeGroup {
		t.Fatalf("no Roles group: %+v", s.nodes)
	}
	if len(g.children) != 2 || s.nodes[g.children[0]].kind != dbNodeRole {
		t.Fatalf("roles = %v", g.children)
	}
	if c := s.nodes["c"].children; len(c) != 2 || c[0] != "c/db/app" {
		t.Fatalf("connection children = %v, want the database then Roles", c)
	}
}

func TestDBTreePartitions(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{"c/t": {kind: dbNodeTable, name: "events", path: "c/t", conn: "c"}}}
	s.addIntrospection("c/t", dbclient.IntrospectionResult{Partitions: []string{"events_2026"}})
	n, ok := s.nodes["c/t/part/events_2026"]
	if !ok || n.name != "Partition · events_2026" {
		t.Fatalf("partition node = %+v", n)
	}
}

func TestDBObjectDDLKind(t *testing.T) {
	for kind, want := range map[dbNodeKind]dbclient.ObjectKind{
		dbNodeRoutine:  dbclient.ObjectFunction,
		dbNodeTrigger:  dbclient.ObjectTrigger,
		dbNodeSequence: dbclient.ObjectSequence,
	} {
		got, ok := dbObjectDDLKind(kind)
		if !ok || got != want {
			t.Fatalf("%v: got %q %v, want %q", kind, got, ok, want)
		}
	}
	if _, ok := dbObjectDDLKind(dbNodeColumn); ok {
		t.Fatal("a column has no object DDL")
	}
}
