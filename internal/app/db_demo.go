package app

import (
	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// databasesDemo prepares stable screenshot states without opening a driver.
func (m *Model) databasesDemo(variant string) {
	s := newDBState()
	s.loaded = true
	s.root = m.databaseWorkspaceRoot()
	m.databases = s
	m.activity.SetActive(activity.ViewDatabases)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	if variant == "empty" {
		s.touch()
		return
	}
	c := dbDemoConnection()
	s.conns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID, detail: "postgres  localhost:5432", loaded: true, children: []string{}}
	if variant == "no-sqlite" {
		s.conns = []dbclient.Conn{{ID: "app-db", Name: "app.db", Driver: "sqlite", Path: "app.db"}}
		note, code := "app-db/sqlite-note", "app-db/sqlite-install"
		s.nodes = map[string]dbNode{"app-db": {kind: dbNodeConnection, name: "app.db", path: "app-db", conn: "app-db", loaded: true, children: []string{note, code}}, note: {kind: dbNodeNote, name: "sqlite3 is not installed or is too old", path: note, conn: "app-db"}, code: {kind: dbNodeCode, name: dbclient.SQLiteInstallHintWithRelease("linux", "amd64", "ID=debian"), path: code, conn: "app-db"}}
		s.cursor = 0
		s.touch()
		return
	}
	conn := "local-pg"
	db := "app"
	schema := "public"
	tables := conn + "/db/" + db + "/schema/" + schema
	group := tables + "/Tables"
	table := group + "/users"
	s.nodes[conn] = dbNode{kind: dbNodeConnection, name: "local-pg", path: conn, conn: conn, detail: "postgres  localhost:5432", loaded: true, children: []string{conn + "/db/" + db}}
	s.nodes[conn+"/db/"+db] = dbNode{kind: dbNodeDatabase, name: db, path: conn + "/db/" + db, conn: conn, loaded: true, children: []string{tables}}
	s.nodes[tables] = dbNode{kind: dbNodeSchema, name: schema, path: tables, conn: conn, loaded: true, children: []string{group, tables + "/Views", tables + "/Routines", tables + "/Sequences"}}
	s.nodes[group] = dbNode{kind: dbNodeGroup, name: "Tables", path: group, conn: conn, loaded: true, children: []string{table}}
	s.nodes[table] = dbNode{kind: dbNodeTable, name: "users", path: table, conn: conn, loaded: true, children: []string{table + "/col/id", table + "/col/email", table + "/col/plan"}}
	for _, g := range []string{"Views", "Routines", "Sequences"} {
		p := tables + "/" + g
		s.nodes[p] = dbNode{kind: dbNodeGroup, name: g, path: p, conn: conn, loaded: true}
	}
	s.nodes[table+"/col/id"] = dbNode{kind: dbNodeColumn, name: "id  bigint", path: table + "/col/id", conn: conn, detail: "PK"}
	s.nodes[table+"/col/email"] = dbNode{kind: dbNodeColumn, name: "email  text", path: table + "/col/email", conn: conn}
	s.nodes[table+"/col/plan"] = dbNode{kind: dbNodeColumn, name: "plan  text", path: table + "/col/plan", conn: conn}
	s.cursor = 4
	s.touch()
}

func dbDemoConnection() dbclient.Conn {
	return dbclient.Conn{ID: "local-pg", Name: "local-pg", Driver: "postgres", Host: "localhost", Port: 5432}
}
