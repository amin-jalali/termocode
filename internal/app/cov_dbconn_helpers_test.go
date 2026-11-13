package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Helpers shared by the db connection / schema / Redis coverage tests.

// cdbModel is a Model whose Databases view loaded the given user
// connections from a private config dir.
func cdbModel(t *testing.T, user ...dbclient.Conn) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if len(user) > 0 {
		p, err := dbUserFile("connections.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := writeDBConfig(p, user); err != nil {
			t.Fatal(err)
		}
	}
	m := &Model{w: 100, h: 30, activity: activity.New(), showExp: true}
	m.ensureDatabasesLoaded()
	m.databases.visible = true
	return m
}

// cdbRealModel is cdbModel on the full model, with a real nvim.
func cdbRealModel(t *testing.T, user ...dbclient.Conn) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	t.Chdir(t.TempDir())
	if len(user) > 0 {
		p, err := dbUserFile("connections.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := writeDBConfig(p, user); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestModel(t)
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	m.w, m.h = 100, 30
	m.databases = nil
	m.ensureDatabasesLoaded()
	m.databases.visible = true
	return &m
}

// cdbAddNode adds a tree node under parent.
func cdbAddNode(s *dbState, parent, path string, kind dbNodeKind, name string) {
	conn := path
	if i := strings.Index(path, "/"); i >= 0 {
		conn = path[:i]
	}
	s.nodes[path] = dbNode{kind: kind, name: name, path: path, conn: conn, loaded: true}
	if parent != "" {
		p := s.nodes[parent]
		p.children = append(p.children, path)
		s.nodes[parent] = p
	}
}

// cdbPGTree is a connection "pg" with db app, schema public, a table, a
// view, a function and a column; every path is returned by name.
func cdbPGTree(s *dbState) {
	cdbAddNode(s, "pg", "pg/db/app", dbNodeDatabase, "app")
	cdbAddNode(s, "pg/db/app", "pg/db/app/schema/public", dbNodeSchema, "public")
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/table/orders", dbNodeTable, "orders")
	cdbAddNode(s, "pg/db/app/schema/public/table/orders", "pg/db/app/schema/public/table/orders/col/id", dbNodeColumn, "id")
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/view/v1", dbNodeView, "v1")
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/routine/f1", dbNodeRoutine, "f1")
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/trigger/t1", dbNodeTrigger, "t1")
}

// cdbPick runs a picker choice through the real router.
func cdbPick(t *testing.T, m *Model, id string) {
	t.Helper()
	if !m.pickerOpen {
		t.Fatalf("no picker open to pick %q", id)
	}
	m.pickerOpen = false
	cmd, ok := m.databaseConsolePicker(id)
	if !ok {
		t.Fatalf("pick %q not routed (kind %v)", id, m.pickerKind)
	}
	cdbRun(cmd)
}

// cdbSubmit runs a prompt answer through the real router.
func cdbSubmit(t *testing.T, m *Model, v string) {
	t.Helper()
	if !m.promptOpen {
		t.Fatalf("no prompt open to submit %q", v)
	}
	m.promptOpen = false
	cmd, ok := m.handleHTTPPromptSubmit(v)
	if !ok {
		t.Fatalf("submit %q not routed (kind %v)", v, m.promptKind)
	}
	cdbRun(cmd)
}

// cdbConfirm runs a card button through the real router.
func cdbConfirm(t *testing.T, m *Model, id string) {
	t.Helper()
	if !m.confirmOpen {
		t.Fatalf("no card open to answer %q", id)
	}
	m.confirmOpen = false
	cmd, ok := m.handleDBConfirm(id)
	if !ok {
		t.Fatalf("answer %q not routed (kind %v)", id, m.confirmKind)
	}
	cdbRun(cmd)
}

// cdbMsgs runs a command (batches flattened) and returns the messages it
// answered within a second; a command that waits longer is dropped.
func cdbMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(time.Second):
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, cdbMsgs(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// cdbRun drops a command: its toast timers are of no interest here.
func cdbRun(tea.Cmd) {}

// cdbSession is a scripted SQL session.
type cdbSession struct {
	dbclient.Session
	mu    sync.Mutex
	sql   []string
	run   func(sql string) dbclient.Result
	intro func(dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error)
	roErr error
	// beginErr and commitErr fail those calls.
	beginErr, commitErr error
}

func (s *cdbSession) Ping(context.Context) error { return nil }
func (s *cdbSession) Close() error               { return nil }
func (s *cdbSession) Cancel()                    {}
func (s *cdbSession) ReadOnlySession() (dbclient.Session, error) {
	if s.roErr != nil {
		return nil, s.roErr
	}
	return s, nil
}
func (s *cdbSession) Introspect(_ context.Context, r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	if s.intro != nil {
		return s.intro(r)
	}
	return dbclient.IntrospectionResult{}, nil
}
func (s *cdbSession) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.sql = append(s.sql, sql)
	s.mu.Unlock()
	if s.run != nil {
		return s.run(sql)
	}
	return dbclient.Result{}
}
func (s *cdbSession) ran() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.sql, "\n")
}

func (s *cdbSession) Begin(context.Context) error    { return s.beginErr }
func (s *cdbSession) Commit(context.Context) error   { return s.commitErr }
func (s *cdbSession) Rollback(context.Context) error { return nil }
