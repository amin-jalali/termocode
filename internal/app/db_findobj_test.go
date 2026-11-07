package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBFindObjectIDRoundTrip(t *testing.T) {
	h := dbFindHit{sessKey: "c/db/app", driver: "postgres", ObjectHit: dbclient.ObjectHit{Conn: "c", Schema: "public", Kind: "column", Name: "id", Parent: "users"}}
	got, ok := dbParseFindID(dbFindID(h))
	if !ok || got != h {
		t.Fatalf("round trip = %+v %v", got, ok)
	}
	if _, ok := dbParseFindID("x"); ok {
		t.Fatal("a bad id parsed")
	}
}

func TestDBFindObjectNodeInTree(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{
		"c":                                   {kind: dbNodeConnection, name: "c", path: "c", conn: "c"},
		"c/db/app":                            {kind: dbNodeDatabase, name: "app", path: "c/db/app", conn: "c"},
		"c/db/app/schema/public":              {kind: dbNodeSchema, name: "public", path: "c/db/app/schema/public", conn: "c"},
		"c/db/app/schema/public/Tables":       {kind: dbNodeGroup, name: "Tables", conn: "c"},
		"c/db/app/schema/public/Tables/users": {kind: dbNodeTable, name: "users", conn: "c"},
	}}
	h := dbFindHit{sessKey: "c/db/app", driver: "postgres", ObjectHit: dbclient.ObjectHit{Conn: "c", Schema: "public", Kind: "column", Name: "id", Parent: "users"}}
	if p := s.findObjectNode(h); p != "c/db/app/schema/public/Tables/users" {
		t.Fatalf("path = %q", p)
	}
	h.Parent, h.Kind, h.Name = "", "table", "orders"
	if p := s.findObjectNode(h); p != "" {
		t.Fatalf("an unloaded object found at %q", p)
	}
}

func TestDBFindObjectPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-find-object"
	}
	if !found {
		t.Fatal("no db-find-object palette item")
	}
	if _, ok := aiActionPolicies["db-find-object"]; !ok {
		t.Fatal("db-find-object has no AI policy")
	}
}
