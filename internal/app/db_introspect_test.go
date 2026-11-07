package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type dbIdleSession struct{ dbclient.Session }

func dbIntrospectTree(t *testing.T) (*Model, string) {
	t.Helper()
	m := &Model{w: 120, h: 40, databases: newDBState()}
	s := m.databases
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg"}
	s.sessions["pg"] = dbIdleSession{}
	s.addIntrospection("pg", dbclient.IntrospectionResult{Databases: []string{"app"}})
	s.addIntrospection("pg/db/app", dbclient.IntrospectionResult{Schemas: []string{"public"}})
	sp := "pg/db/app/schema/public"
	s.addIntrospection(sp, dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}}})
	return m, sp + "/Tables/users"
}

// DBX-29: db_introspect_level "objects" keeps the columns of a table but
// not its indexes and foreign keys.
func TestDBIntrospectLevelTrimsTableDetail(t *testing.T) {
	writeDBDisplayConfig(t, `{"db_introspect_level":"objects"}`)
	m, tp := dbIntrospectTree(t)
	m.databases.addIntrospection(tp, dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "int", PK: true}}, Indexes: []dbclient.SchemaIndex{{Name: "users_pkey", Columns: []string{"id"}}}})
	if _, ok := m.databases.nodes[tp+"/col/id"]; !ok {
		t.Fatal("columns dropped at level objects")
	}
	if _, ok := m.databases.nodes[tp+"/index/users_pkey"]; ok {
		t.Fatal("index kept at level objects")
	}
}

// DBX-29: refreshing one tree object reloads only that node.
func TestDBRefreshOneObject(t *testing.T) {
	withConfigDir(t)
	m, tp := dbIntrospectTree(t)
	m.databases.addIntrospection(tp, dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "int"}}})
	if m.databaseRefreshNode(tp) == nil {
		t.Fatal("no reload command")
	}
	n := m.databases.nodes[tp]
	if !n.loading || len(n.children) != 0 {
		t.Fatalf("node not reloading: %+v", n)
	}
	if _, ok := m.databases.nodes[tp+"/col/id"]; ok {
		t.Fatal("old column row kept")
	}
	if _, ok := m.databases.nodes["pg/db/app/schema/public"]; !ok {
		t.Fatal("the parent was dropped")
	}
}

// DBX-29: the schema cache remembers each load; a load that differs from
// the cached one marks the node "changed".
func TestDBSchemaCacheMarksChanged(t *testing.T) {
	withConfigDir(t)
	m, tp := dbIntrospectTree(t)
	first := dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "int"}}}
	msg := m.databaseCacheSchema(tp, first)().(dbSchemaCacheMsg)
	if msg.changed {
		t.Fatal("first load marked changed")
	}
	msg = m.databaseCacheSchema(tp, first)().(dbSchemaCacheMsg)
	if msg.changed {
		t.Fatal("same schema marked changed")
	}
	second := dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "bigint"}}}
	msg = m.databaseCacheSchema(tp, second)().(dbSchemaCacheMsg)
	if !msg.changed {
		t.Fatal("a changed column type was not seen")
	}
	m.onDBSchemaCache(msg)
	if d := m.databases.nodes[tp].detail; !strings.Contains(d, "changed") {
		t.Fatalf("detail %q", d)
	}
	if _, ok := m.dbSchemaCache().LastRefresh("pg"); !ok {
		t.Fatal("no last refresh time")
	}
}

// DBX-29: db_complete_catalogs adds the system catalogs to completion.
func TestDBCompletionSystemCatalogs(t *testing.T) {
	writeDBDisplayConfig(t, `{"db_complete_catalogs":true}`)
	m, _ := dbIntrospectTree(t)
	c, _ := m.databases.connection("pg")
	p := m.databases.schemaPayload(c)
	found := false
	for _, it := range p["tables"].([]map[string]any) {
		if it["n"] == "pg_catalog.pg_class" {
			found = true
		}
	}
	if !found {
		t.Fatal("pg_catalog.pg_class missing from completion")
	}
}
