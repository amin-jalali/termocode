package app

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

type fakeDBSession struct {
	dbclient.Session
	closed  atomic.Int32
	objects []dbclient.SchemaObject
}

func (f *fakeDBSession) Close() error { f.closed.Add(1); return nil }
func (f *fakeDBSession) Introspect(context.Context, dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return dbclient.IntrospectionResult{Objects: f.objects}, nil
}

func dbRowNames(s *dbState) string {
	var b strings.Builder
	for _, r := range s.rows() {
		b.WriteString(r.name + "\n")
	}
	return b.String()
}

func dbTestModel(s *dbState) *Model {
	m := &Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	return m
}

// Introspection results arrive through onDBIntrospect; a Postgres schema and
// a MySQL database must then show their tables.
func TestDatabaseIntrospectionKeepsTablesOfSchemaAndMySQLDatabase(t *testing.T) {
	objs := []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active_users", Kind: "VIEW"}}
	for _, tc := range []struct{ name, driver, path string }{
		{"postgres schema", "postgres", "pg/db/app/schema/public"},
		{"mysql database", "mysql", "my/db/app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := strings.SplitN(tc.path, "/", 2)[0]
			s := newDBState()
			s.visible = true
			s.conns = []dbclient.Conn{{ID: conn, Driver: tc.driver}}
			s.nodes[conn] = dbNode{kind: dbNodeConnection, name: conn, path: conn, conn: conn}
			m := dbTestModel(s)
			m.onDBIntrospect(dbIntrospectMsg{path: conn, generation: s.generation, result: dbclient.IntrospectionResult{Databases: []string{"app"}}})
			dbPath := conn + "/db/app"
			if tc.driver == "postgres" {
				m.onDBIntrospect(dbIntrospectMsg{path: dbPath, generation: s.generation, result: dbclient.IntrospectionResult{Schemas: []string{"public"}}})
			}
			m.onDBIntrospect(dbIntrospectMsg{path: tc.path, generation: s.generation, result: dbclient.IntrospectionResult{Objects: objs}})
			names := dbRowNames(s)
			for _, want := range []string{"Tables", "Views", "users", "active_users"} {
				if !strings.Contains(names, want+"\n") {
					t.Fatalf("row %q missing from tree:\n%s", want, names)
				}
			}
		})
	}
}

func TestDatabaseExpandOtherDatabaseOpensOneSessionAndReuses(t *testing.T) {
	c := dbclient.Conn{ID: "local", Driver: "postgres", Database: "default"}
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{c}
	s.passwords[c.ID] = "pw"
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID, loaded: true}
	dbPath := c.ID + "/db/other"
	s.nodes[dbPath] = dbNode{kind: dbNodeDatabase, name: "other", path: dbPath, conn: c.ID}
	base := &fakeDBSession{}
	s.sessions[c.ID] = base
	s.activeID = c.ID
	var opened []*fakeDBSession
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		f := &fakeDBSession{objects: []dbclient.SchemaObject{{Name: "t1", Kind: "BASE TABLE"}}}
		opened = append(opened, f)
		return f, nil
	}
	defer func() { dbSQLSessionFactory = old }()
	m := dbTestModel(s)

	// Drive the whole command chain; it must end in a bounded number of steps.
	cmd := m.databaseExpand(dbPath)
	for i := 0; cmd != nil; i++ {
		if i > 5 {
			t.Fatal("expand keeps chaining commands (connect loop)")
		}
		switch msg := cmd().(type) {
		case dbConnectMsg:
			cmd = m.onDBConnect(msg)
		case dbIntrospectMsg:
			cmd = m.onDBIntrospect(msg)
		default:
			t.Fatalf("unexpected message %T", msg)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("expanding another database opened %d sessions, want 1", len(opened))
	}
	if s.activeID != c.ID {
		t.Fatalf("activeID = %q, want the connection id", s.activeID)
	}
	if s.sessions[dbPath] != opened[0] || s.sessions[c.ID] != base || base.closed.Load() != 0 {
		t.Fatal("sessions were overwritten or the base session was closed")
	}

	// Refold and expand again: the existing session is reused.
	n := s.nodes[dbPath]
	n.loaded = false
	s.nodes[dbPath] = n
	cmd = m.databaseExpand(dbPath)
	if cmd == nil {
		t.Fatal("re-expanding returned no command")
	}
	if _, ok := cmd().(dbIntrospectMsg); !ok {
		t.Fatal("re-expanding should introspect with the existing session")
	}
	if len(opened) != 1 {
		t.Fatalf("re-expanding opened a session (total %d)", len(opened))
	}

	// A late duplicate connect closes the session it replaces.
	dup := &fakeDBSession{}
	_ = m.onDBConnect(dbConnectMsg{id: dbPath, generation: s.generation, session: dup, password: "pw"})
	if opened[0].closed.Load() != 1 || s.sessions[dbPath] != dup {
		t.Fatalf("replaced session closed %d times", opened[0].closed.Load())
	}

	// Disconnect closes every session of the connection.
	s.pendingDisconnectID = c.ID
	m.confirmKind = confirmKindDBDisconnect
	m.handleDBConfirm("disconnect")
	if base.closed.Load() != 1 || dup.closed.Load() != 1 || len(s.sessions) != 0 {
		t.Fatalf("disconnect: base=%d dup=%d left=%d", base.closed.Load(), dup.closed.Load(), len(s.sessions))
	}
}

// Spec: after the Databases view is hidden nothing stays open or running.
func TestPerfGuardDatabasesIdleAfterHide(t *testing.T) {
	a, b := &fakeDBSession{}, &fakeDBSession{}
	s := newDBState()
	s.visible = true
	s.sessions["local"] = a
	s.sessions["local/db/other"] = b
	m := dbTestModel(s)
	before := runtime.NumGoroutine()
	updated, _ := m.Update(activity.SwitchMsg{View: activity.ViewFiles})
	got := updated.(Model)
	if len(got.databases.sessions) != 0 || a.closed.Load() != 1 || b.closed.Load() != 1 {
		t.Fatal("sessions still open after hiding the view")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		next, _ := got.Update(aiActivityTickMsg{})
		got = next.(Model)
		if n := runtime.NumGoroutine(); n > before {
			t.Fatalf("goroutines grew while idle: %d -> %d", before, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got.databases.sessions) != 0 {
		t.Fatal("a session was reopened while idle")
	}
}
