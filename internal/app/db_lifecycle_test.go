package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

type pingFailureDBSession struct{ fakeDBSession }

func (*pingFailureDBSession) Ping(context.Context) error { return errors.New("ping failed") }

func TestDatabaseTestClosesSessionWhenPingFails(t *testing.T) {
	sess := &pingFailureDBSession{}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return sess, nil }
	t.Cleanup(func() { dbSQLSessionFactory = old })
	msg := dbTestCommand(dbclient.Conn{Driver: "postgres"}, "", t.TempDir(), 0)().(dbTestMsg)
	if msg.err == nil || sess.closed.Load() != 1 {
		t.Fatalf("ping failure: err=%v closed=%d, want error and one close", msg.err, sess.closed.Load())
	}
}

func TestDatabaseLateConnectClosesWithoutModel(t *testing.T) {
	sess := &fakeDBSession{}
	m := Model{}
	m.onDBConnect(dbConnectMsg{session: sess})
	if sess.closed.Load() != 1 {
		t.Fatal("late connect leaked its session after database state disappeared")
	}
}

func TestDatabaseDisconnectRejectsInFlightChildConnect(t *testing.T) {
	s := newDBState()
	s.visible = true
	c := dbclient.Conn{ID: "local", Driver: "postgres", Database: "app"}
	s.conns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID}
	s.addIntrospection(c.ID, dbclient.IntrospectionResult{Databases: []string{"other"}})
	s.sessions[c.ID] = &fakeDBSession{}
	m := dbTestModel(s)
	opened := &fakeDBSession{}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return opened, nil }
	t.Cleanup(func() { dbSQLSessionFactory = old })
	cmd := m.databaseExpand("local/db/other")
	s.pendingDisconnectID = c.ID
	m.confirmKind = confirmKindDBDisconnect
	m.handleDBConfirm("disconnect")
	// Deliver the connection that was already running when disconnect was accepted.
	m.onDBConnect(cmd().(dbConnectMsg))
	if len(s.sessions) != 0 || opened.closed.Load() != 1 {
		t.Fatalf("late connection reopened disconnected database: sessions=%d closed=%d", len(s.sessions), opened.closed.Load())
	}
}

func TestDatabaseRepeatedEnterDoesNotOpenWhileConnecting(t *testing.T) {
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{{ID: "local", Driver: "postgres", Database: "app"}}
	s.nodes["local"] = dbNode{kind: dbNodeConnection, path: "local", conn: "local"}
	m := dbTestModel(s)
	updated, first := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	if first == nil {
		t.Fatal("first Enter did not connect")
	}
	_, second := updated.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	if second != nil {
		t.Fatal("second Enter started another connection while the first was running")
	}
}

func TestDatabaseTargetSessionOverridesConnectionURL(t *testing.T) {
	c, err := dbclient.ParseURL("postgres://app@localhost/default?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	c.ID = "local"
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, path: c.ID, conn: c.ID}
	s.addIntrospection(c.ID, dbclient.IntrospectionResult{Databases: []string{"other"}})
	s.sessions[c.ID] = &fakeDBSession{}
	m := dbTestModel(s)
	old := dbSQLSessionFactory
	var dsn string
	dbSQLSessionFactory = func(_ context.Context, conn dbclient.Conn, password string) (dbclient.Session, error) {
		dsn = conn.DSN(password)
		return nil, errors.New("stop after checking DSN")
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	m.databaseExpand("local/db/other")()
	if !strings.Contains(dsn, "/other?") || !strings.Contains(dsn, "sslmode=disable") {
		t.Fatalf("target session lost target database or URL options: %s", dsn)
	}
}
